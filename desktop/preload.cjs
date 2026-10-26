const { contextBridge, ipcRenderer } = require('electron');

// No backend token, filesystem, process, generic IPC or shell access crosses
// this bridge. Main validates both the sender and the current document.
contextBridge.exposeInMainWorld('timoneDesktop', Object.freeze({
  configure: () => ipcRenderer.invoke('desktop:configure'),
  reconnect: () => ipcRenderer.invoke('desktop:reconnect'),
  update: Object.freeze({
    state: () => ipcRenderer.invoke('desktop:update-state'),
    check: () => ipcRenderer.invoke('desktop:update-check'),
    install: () => ipcRenderer.invoke('desktop:update-install'),
    openReleasePage: () => ipcRenderer.invoke('desktop:update-open-release'),
    onEvent: (callback) => {
      if (typeof callback !== 'function') throw new TypeError('Update listener must be a function.');
      const listener = (_event, state) => callback(state);
      ipcRenderer.on('desktop:update-event', listener);
      return () => ipcRenderer.removeListener('desktop:update-event', listener);
    },
  }),
}));

if (window.location.protocol === 'file:') {
  contextBridge.exposeInMainWorld('timoneSetup', Object.freeze({
    settings: () => ipcRenderer.invoke('desktop:settings'),
    chooseKubeconfig: () => ipcRenderer.invoke('desktop:choose-kubeconfig'),
    connect: (settings) => ipcRenderer.invoke('desktop:connect', settings),
  }));
}
