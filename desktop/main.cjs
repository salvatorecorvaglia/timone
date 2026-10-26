const { app, BrowserWindow, dialog, ipcMain, Menu, session, shell } = require('electron');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const { randomUUID } = require('node:crypto');
const { Backend } = require('./backend.cjs');
const { readSettings, saveSettings, validateSettings } = require('./settings.cjs');
const { isExternalURL, sameOrigin } = require('./security.cjs');
const {
  initAutoUpdater,
  getUpdateState,
  checkForUpdate,
  installUpdate,
  openReleasePage,
} = require('./updater.cjs');

const welcomePath = path.join(__dirname, 'welcome.html');
const welcomeURL = pathToFileURL(welcomePath).href;
const welcomeResources = new Set(['welcome.html', 'welcome.css', 'palette.css', 'welcome.js', 'assets/icon.svg'].map((file) => pathToFileURL(path.join(__dirname, file)).href));
let window;
let backend;
let desktopSession;
let settings;
let busy = false;
let quitting = false;

if (!app.requestSingleInstanceLock()) app.quit();
else {
  app.on('second-instance', () => {
    if (window) { if (window.isMinimized()) window.restore(); window.show(); window.focus(); }
  });
  app.whenReady().then(start).catch((error) => {
    console.error('Timone desktop initialization failed:', String(error.message).slice(0, 1024));
    dialog.showErrorBox('Timone could not start', 'The desktop application could not initialize. Reinstall Timone and try again.');
    app.quit();
  });
}

function trusted(event, setupOnly = false) {
  if (!window || event.sender !== window.webContents || event.senderFrame !== window.webContents.mainFrame) return false;
  const url = event.senderFrame.url;
  return url === welcomeURL || (!setupOnly && sameOrigin(url, backend?.origin));
}

function handle(channel, setupOnly, handler) {
  ipcMain.handle(channel, async (event, ...args) => {
    if (!trusted(event, setupOnly)) throw new Error('Untrusted desktop request.');
    try { return { ok: true, value: await handler(...args) }; }
    catch (error) { return { ok: false, error: String(error.message).slice(0, 1024) }; }
  });
}

async function stopBackend() {
  const previous = backend;
  backend = undefined;
  if (previous) await previous.stop();
  await desktopSession.clearStorageData();
}

async function configure() {
  if (busy) throw new Error('Wait for the connection to finish.');
  busy = true;
  try {
    // Finish shutdown before making the setup form interactive. The previous
    // origin is revoked immediately by stopBackend's request filter state.
    await stopBackend();
  } finally { busy = false; }
  await window.loadFile(welcomePath);
}

async function authenticate() {
  if (!backend || backend.stopping) throw new Error('Choose a connection first.');
  const value = await backend.login();
  await desktopSession.cookies.set({ url: backend.origin, name: 'timone_session', value, httpOnly: true, secure: false, sameSite: 'strict', path: '/' });
}

async function connect(input) {
  if (busy) throw new Error('A connection is already starting.');
  // The bundled setup page may choose a context and grant exec consent. The
  // backend web app cannot select paths or enable credential plugins via IPC.
  const next = validateSettings({ ...input, kubeconfig: settings.kubeconfig });
  if (app.isPackaged && next.mode === 'demo') throw new Error('Demo mode is available only in development.');
  busy = true;
  let candidate;
  try {
    await stopBackend();
    const executable = process.platform === 'win32' ? 'timone.exe' : 'timone';
    const binary = path.join(app.isPackaged ? process.resourcesPath : path.join(__dirname, 'resources'), executable);
    candidate = new Backend(binary, next, { env: { ...process.env, TIMONE_ENV: app.isPackaged ? 'production' : 'development' } });
    backend = candidate;
    await candidate.ready;
    await authenticate();
    // Settings contain only connection preferences, never credentials.
    await saveSettings(app.getPath('userData'), next);
    settings = next;
    await window.loadURL(candidate.origin);
    candidate.on('stopped', () => {
      if (backend !== candidate || quitting) return;
      backend = undefined;
      void window.loadFile(welcomePath).then(() => {
        if (!quitting) dialog.showMessageBox(window, { type: 'error', title: 'Connection stopped', message: 'The Timone backend stopped. Choose a connection to start it again.' });
      });
    });
  } catch (error) {
    if (candidate) await candidate.stop();
    if (backend === candidate) backend = undefined;
    throw error;
  } finally { busy = false; }
}

async function start() {
  app.setName('Timone');
  settings = await readSettings(app.getPath('userData'));
  desktopSession = session.fromPartition(`timone-${randomUUID()}`);
  desktopSession.setPermissionRequestHandler((_contents, permission, callback) => callback(permission === 'clipboard-sanitized-write'));
  desktopSession.setPermissionCheckHandler((_contents, permission) => permission === 'clipboard-sanitized-write');
  desktopSession.webRequest.onBeforeRequest((details, callback) => {
    const allowed = welcomeResources.has(details.url) || sameOrigin(details.url, backend?.origin) ||
      (backend?.origin && details.url.startsWith(`blob:${backend.origin}/`));
    callback({ cancel: !allowed });
  });
  window = new BrowserWindow({
    width: 1440, height: 940, minWidth: 1040, minHeight: 700,
    title: 'Timone', backgroundColor: '#111518', show: false,
    icon: path.join(__dirname, 'assets/icon.png'),
    webPreferences: {
      session: desktopSession, preload: path.join(__dirname, 'preload.cjs'),
      nodeIntegration: false, contextIsolation: true, sandbox: true,
      webSecurity: true, allowRunningInsecureContent: false, webviewTag: false,
    },
  });
  window.webContents.on('will-navigate', (event, url) => {
    if (sameOrigin(url, backend?.origin) || url === welcomeURL) return;
    event.preventDefault();
    if (isExternalURL(url)) void shell.openExternal(url);
  });
  window.webContents.on('will-redirect', (event, url) => {
    if (!sameOrigin(url, backend?.origin)) event.preventDefault();
  });
  window.webContents.setWindowOpenHandler(({ url }) => {
    if (isExternalURL(url)) void shell.openExternal(url);
    return { action: 'deny' };
  });
  window.webContents.on('will-attach-webview', (event) => event.preventDefault());
  window.webContents.on('render-process-gone', () => { if (!quitting) void configure().catch(() => {}); });
  handle('desktop:settings', true, () => ({ ...settings, demoEnabled: !app.isPackaged }));
  handle('desktop:choose-kubeconfig', true, async () => {
    if (busy) throw new Error('Wait for the connection to finish.');
    const result = await dialog.showOpenDialog(window, { title: 'Choose kubeconfig', properties: ['openFile'] });
    if (!result.canceled && result.filePaths[0] && result.filePaths[0] !== settings.kubeconfig) {
      settings.kubeconfig = result.filePaths[0];
      settings.allowExecAuth = false;
    }
    return { kubeconfig: settings.kubeconfig, allowExecAuth: settings.allowExecAuth };
  });
  handle('desktop:connect', true, connect);
  handle('desktop:configure', false, configure);
  handle('desktop:reconnect', false, async () => { await authenticate(); return true; });
  handle('desktop:update-state', false, () => getUpdateState());
  handle('desktop:update-check', false, () => checkForUpdate({ manual: true }));
  handle('desktop:update-install', false, () => installUpdate());
  handle('desktop:update-open-release', false, () => openReleasePage());
  Menu.setApplicationMenu(Menu.buildFromTemplate([
    ...(process.platform === 'darwin' ? [{ role: 'appMenu' }] : []),
    { label: 'Connection', submenu: [{ label: 'Change connection…', click: () => { void configure().catch(() => {}); } }, { type: 'separator' }, { role: 'quit' }] },
    { role: 'editMenu' }, { role: 'viewMenu' }, { role: 'windowMenu' },
    { role: 'help', submenu: [{ label: 'Timone releases', click: () => { void shell.openExternal('https://github.com/salvatorecorvaglia/timone/releases'); } }] },
  ]));
  initAutoUpdater({
    onState: (state) => {
      if (window && !window.isDestroyed()) window.webContents.send('desktop:update-event', state);
    },
  });
  await window.loadFile(welcomePath);
  window.show();
}

app.on('window-all-closed', () => app.quit());
app.on('before-quit', (event) => {
  if (quitting) return;
  event.preventDefault();
  quitting = true;
  void (async () => { if (backend) await backend.stop(); app.quit(); })();
});
