const setup = window.timoneSetup;
const form = document.querySelector('#connection');
const contextInput = document.querySelector('#context');
const execInput = document.querySelector('#exec');
const fileLabel = document.querySelector('#kubeconfig');
const errorLabel = document.querySelector('#error');
const statusLabel = document.querySelector('#status');

function showError(message) {
  errorLabel.textContent = message;
  errorLabel.hidden = !message;
}

setup.settings().then((result) => {
  if (!result.ok) { showError(result.error); return; }
  document.querySelector('#demo').hidden = !result.value.demoEnabled;
  contextInput.value = result.value.context;
  execInput.checked = result.value.allowExecAuth;
  if (result.value.kubeconfig) fileLabel.textContent = result.value.kubeconfig;
});

document.querySelector('#choose').addEventListener('click', async () => {
  const result = await setup.chooseKubeconfig();
  if (!result.ok) showError(result.error);
  else {
    fileLabel.textContent = result.value.kubeconfig || 'Use KUBECONFIG or ~/.kube/config';
    execInput.checked = result.value.allowExecAuth;
  }
});

async function connect(mode) {
  showError('');
  statusLabel.textContent = mode === 'demo' ? 'Starting the demo…' : 'Connecting to your cluster…';
  for (const element of form.elements) element.disabled = true;
  try {
    const result = await setup.connect({ mode, context: contextInput.value, allowExecAuth: execInput.checked });
    if (!result.ok) showError(result.error);
  } catch { showError('Connection could not be opened. Please try again.'); }
  finally {
    for (const element of form.elements) element.disabled = false;
    statusLabel.textContent = '';
  }
}

form.addEventListener('submit', (event) => { event.preventDefault(); void connect('local'); });
document.querySelector('#demo').addEventListener('click', () => { void connect('demo'); });
