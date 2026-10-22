const { contextBridge, ipcRenderer } = require('electron');

// No backend token, filesystem, process, generic IPC or shell access crosses
// this bridge. Main validates both the sender and the current document.
contextBridge.exposeInMainWorld('timoneDesktop', Object.freeze({
  configure: () => ipcRenderer.invoke('desktop:configure'),
  reconnect: () => ipcRenderer.invoke('desktop:reconnect'),
}));

if (window.location.protocol === 'file:') {
  contextBridge.exposeInMainWorld('timoneSetup', Object.freeze({
    settings: () => ipcRenderer.invoke('desktop:settings'),
    chooseKubeconfig: () => ipcRenderer.invoke('desktop:choose-kubeconfig'),
    connect: (settings) => ipcRenderer.invoke('desktop:connect', settings),
  }));
}
