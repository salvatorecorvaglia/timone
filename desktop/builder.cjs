module.exports = {
  appId: 'io.github.salvatorecorvaglia.timone',
  productName: 'Timone',
  asar: true,
  npmRebuild: false,
  directories: { output: 'dist', buildResources: 'assets' },
  files: ['main.cjs', 'updater.cjs', 'mac-self-update.cjs', 'backend.cjs', 'settings.cjs', 'security.cjs', 'preload.cjs', 'welcome.html', 'welcome.js', 'welcome.css', 'palette.css', 'assets/icon.svg', 'assets/icon.png', 'package.json'],
  extraResources: [{ from: 'resources', to: '.', filter: ['timone', 'timone.exe'] }],
  artifactName: 'timone-desktop_${version}_${os}_${arch}.${ext}',
  // The installed app reads its feed from GitHub, but publishing stays in the
  // release workflow: electron-builder writes latest*.yml here (even with
  // --publish never) and the workflow uploads them with the installers.
  publish: {
    provider: 'generic',
    url: 'https://github.com/salvatorecorvaglia/timone/releases/latest/download',
  },
  generateUpdatesFilesForAllChannels: false,
  mac: {
    category: 'public.app-category.developer-tools',
    icon: 'assets/icon.png',
    target: ['dmg', 'zip'],
    identity: '-',
    hardenedRuntime: true,
    notarize: false,
    gatekeeperAssess: false,
  },
  win: { executableName: 'timone-desktop', icon: 'assets/icon.ico', target: ['nsis'] },
  nsis: { oneClick: false, perMachine: false, allowToChangeInstallationDirectory: true },
  linux: { icon: 'assets/icon.png', target: ['deb', 'AppImage'], category: 'Development', maintainer: 'Salvatore Corvaglia', executableName: 'timone-desktop' },
  deb: { depends: ['libgtk-3-0', 'libnss3', 'libxss1', 'libxtst6', 'xdg-utils', 'libgbm1', 'libasound2t64 | libasound2'] },
};
