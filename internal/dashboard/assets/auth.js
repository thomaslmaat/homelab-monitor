(() => {
  const overlay = document.createElement('div');
  overlay.id = 'auth-gate';
  overlay.className = 'auth-gate';
  overlay.innerHTML = `
    <div class="auth-card">
      <div class="auth-brand"><div class="auth-mark">H</div><div><strong>homelab<span>.</span></strong><small>MONITOR ACCESS</small></div></div>
      <div class="auth-eyebrow">SECURE WORKSPACE</div>
      <h1 id="auth-title">Inloggen</h1>
      <p id="auth-description">Log in om je homelab-dashboard te openen.</p>
      <form id="auth-form" autocomplete="on">
        <label for="auth-username" id="auth-user-label">Gebruikersnaam</label>
        <input id="auth-username" name="username" autocomplete="username" value="admin" required>
        <label for="auth-password" id="auth-pass-label">Wachtwoord</label>
        <input id="auth-password" name="password" type="password" autocomplete="current-password" required>
        <div id="auth-new-fields" class="auth-hidden">
          <label for="auth-new-password">Nieuw wachtwoord</label>
          <input id="auth-new-password" type="password" autocomplete="new-password" minlength="12">
          <label for="auth-confirm-password">Herhaal nieuw wachtwoord</label>
          <input id="auth-confirm-password" type="password" autocomplete="new-password" minlength="12">
        </div>
        <p id="auth-message" class="auth-message" role="alert"></p>
        <button id="auth-submit" class="auth-submit" type="submit">Inloggen</button>
      </form>
      <p id="auth-default-hint" class="auth-hint">Eerste installatie: gebruikersnaam <b>admin</b>, wachtwoord <b>admin</b>. Je moet dit wachtwoord direct wijzigen.</p>
      <p class="auth-foot">Je sessie verloopt automatisch na 12 uur.</p>
    </div>`;
  document.body.appendChild(overlay);

  let mode = 'login';
  const $ = id => document.getElementById(id);
  const form = $('auth-form');
  const message = $('auth-message');
  const submit = $('auth-submit');

  function showLogin() {
    mode = 'login';
    $('auth-title').textContent = 'Inloggen';
    $('auth-description').textContent = 'Log in om je homelab-dashboard te openen.';
    $('auth-user-label').classList.remove('auth-hidden');
    $('auth-username').classList.remove('auth-hidden');
    $('auth-pass-label').textContent = 'Wachtwoord';
    $('auth-password').autocomplete = 'current-password';
    $('auth-new-fields').classList.add('auth-hidden');
    $('auth-default-hint').classList.remove('auth-hidden');
    submit.textContent = 'Inloggen';
    $('auth-password').value = '';
  }
  function showPasswordChange() {
    mode = 'change';
    $('auth-title').textContent = 'Wijzig je wachtwoord';
    $('auth-description').textContent = 'Voor de veiligheid moet je het standaardwachtwoord eerst vervangen.';
    $('auth-user-label').classList.add('auth-hidden');
    $('auth-username').classList.add('auth-hidden');
    $('auth-pass-label').textContent = 'Huidig wachtwoord';
    $('auth-password').autocomplete = 'current-password';
    $('auth-new-fields').classList.remove('auth-hidden');
    $('auth-default-hint').classList.add('auth-hidden');
    submit.textContent = 'Wachtwoord opslaan';
  }
  function enterDashboard(username) {
    document.body.classList.remove('auth-pending');
    document.body.classList.add('auth-ready');
    overlay.remove();
    const actions = document.querySelector('.top-actions');
    if (actions && !document.getElementById('auth-logout')) {
      const button = document.createElement('button');
      button.id = 'auth-logout';
      button.className = 'auth-logout';
      button.textContent = 'Uitloggen';
      button.title = 'Uitloggen als ' + username;
      button.addEventListener('click', async () => {
        await fetch('/api/v1/auth/logout', {method:'POST', credentials:'same-origin'});
        location.reload();
      });
      actions.appendChild(button);
    }
  }
  async function checkSession() {
    try {
      const response = await fetch('/api/v1/auth/status', {credentials:'same-origin', cache:'no-store'});
      if (!response.ok) throw new Error('Authenticatiestatus kon niet worden geladen.');
      const data = await response.json();
      if (!data.authenticated) {
        showLogin();
        document.body.classList.remove('auth-pending');
        return;
      }
      if (data.must_change_password) {
        showPasswordChange();
        document.body.classList.remove('auth-pending');
        return;
      }
      enterDashboard(data.username || 'admin');
    } catch (error) {
      showLogin();
      message.textContent = error.message;
      document.body.classList.remove('auth-pending');
    }
  }
  form.addEventListener('submit', async event => {
    event.preventDefault();
    message.textContent = '';
    submit.disabled = true;
    try {
      if (mode === 'login') {
        const response = await fetch('/api/v1/auth/login', {
          method:'POST', credentials:'same-origin',
          headers:{'Content-Type':'application/json'},
          body:JSON.stringify({username:$('auth-username').value.trim(), password:$('auth-password').value})
        });
        const data = await response.json();
        if (!response.ok) throw new Error(data.error || 'Inloggen mislukt.');
        await checkSession();
      } else {
        const current = $('auth-password').value;
        const next = $('auth-new-password').value;
        const confirm = $('auth-confirm-password').value;
        if (next.length < 12) throw new Error('Gebruik minimaal 12 tekens voor je nieuwe wachtwoord.');
        if (next !== confirm) throw new Error('De nieuwe wachtwoorden komen niet overeen.');
        if (next === 'admin') throw new Error('Kies niet opnieuw het standaardwachtwoord.');
        const response = await fetch('/api/v1/auth/password', {
          method:'POST', credentials:'same-origin',
          headers:{'Content-Type':'application/json'},
          body:JSON.stringify({current_password:current,new_password:next})
        });
        const data = await response.json();
        if (!response.ok) throw new Error(data.error || 'Wachtwoord wijzigen mislukt.');
        location.reload();
      }
    } catch (error) {
      message.textContent = error.message;
    } finally {
      submit.disabled = false;
    }
  });
  checkSession();
})();