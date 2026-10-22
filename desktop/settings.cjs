const fs = require('node:fs/promises');
const path = require('node:path');

function validateSettings(value) {
  if (!value || typeof value !== 'object' || !['local', 'demo'].includes(value.mode)) throw new Error('Choose a cluster or the demo.');
  const context = value.context ?? '';
  const kubeconfig = value.kubeconfig ?? '';
  if (typeof context !== 'string' || context.length > 1024 || /[\x00-\x1f\x7f]/.test(context)) throw new Error('Invalid context name.');
  if (typeof kubeconfig !== 'string' || kubeconfig.length > 8192 || kubeconfig.includes('\0')) throw new Error('Invalid kubeconfig path.');
  return { mode: value.mode, context, kubeconfig, allowExecAuth: value.allowExecAuth === true };
}

async function readSettings(directory) {
  try {
    const file = path.join(directory, 'connection.json');
    if ((await fs.stat(file)).size > 65536) throw new Error('Settings too large');
    return validateSettings(JSON.parse(await fs.readFile(file, 'utf8')));
  } catch { return { mode: 'local', context: '', kubeconfig: '', allowExecAuth: false }; }
}

async function saveSettings(directory, settings) {
  const value = validateSettings(settings);
  await fs.mkdir(directory, { recursive: true, mode: 0o700 });
  const file = path.join(directory, 'connection.json');
  const temporary = `${file}.tmp`;
  await fs.writeFile(temporary, JSON.stringify(value) + '\n', { mode: 0o600 });
  await fs.rename(temporary, file);
}

module.exports = { validateSettings, readSettings, saveSettings };
