module.exports = {
  appId: 'io.github.salvatorecorvaglia.timone',
  productName: 'Timone',
  asar: true,
  npmRebuild: false,
  forceCodeSigning: process.env.TIMONE_REQUIRE_SIGNING === 'true',
  directories: { output: 'dist', buildResources: 'assets' },
  files: ['main.cjs', 'backend.cjs', 'settings.cjs', 'security.cjs', 'preload.cjs', 'welcome.html', 'welcome.js', 'welcome.css', 'palette.css', 'assets/icon.svg', 'assets/icon.png', 'package.json'],
  extraResources: [{ from: 'resources', to: '.', filter: ['timone', 'timone.exe'] }],
  artifactName: 'timone-desktop_${version}_${os}_${arch}.${ext}',
  // Distribution is explicit in GitHub Actions. No background updater or
  // third-party publishing service runs from the installed application.
  publish: null,
  mac: {
    category: 'public.app-category.developer-tools',
    icon: 'assets/icon.png',
    target: ['dmg', 'zip'],
    identity: process.env.APPLE_SIGNING_IDENTITY || (process.env.CSC_LINK ? undefined : '-'),
    hardenedRuntime: true,
    notarize: Boolean(process.env.APPLE_ID && process.env.APPLE_APP_SPECIFIC_PASSWORD && process.env.APPLE_TEAM_ID),
    gatekeeperAssess: false,
  },
  win: { executableName: 'timone-desktop', icon: 'assets/icon.ico', target: ['nsis'] },
  nsis: { oneClick: false, perMachine: false, allowToChangeInstallationDirectory: true },
  linux: { icon: 'assets/icon.png', target: ['deb', 'AppImage'], category: 'Development', maintainer: 'Salvatore Corvaglia', executableName: 'timone-desktop' },
  deb: { depends: ['libgtk-3-0', 'libnss3', 'libxss1', 'libxtst6', 'xdg-utils', 'libgbm1', 'libasound2t64 | libasound2'] },
};
