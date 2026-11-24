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

  function showLogin(defaultPasswordAvailable = false) {
    mode = 'login';
    $('auth-title').textContent = 'Inloggen';
    $('auth-description').textContent = 'Log in om je homelab-dashboard te openen.';
    $('auth-user-label').classList.remove('auth-hidden');
    $('auth-username').classList.remove('auth-hidden');
    $('auth-pass-label').textContent = 'Wachtwoord';
    $('auth-password').autocomplete = 'current-password';
    $('auth-new-fields').classList.add('auth-hidden');
    $('auth-default-hint').classList.toggle('auth-hidden', !defaultPasswordAvailable);
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
  function enterDashboard(username, auth) {
    document.body.classList.remove('auth-pending');
    document.body.classList.add('auth-ready');
    overlay.remove();
    configurePermissions(auth || {});
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
        showLogin(data.default_password_available === true);
        document.body.classList.remove('auth-pending');
        return;
      }
      if (data.must_change_password) {
        showPasswordChange();
        document.body.classList.remove('auth-pending');
        return;
      }
      enterDashboard(data.username || 'admin', data);
    } catch (error) {
      showLogin(false);
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

  function configurePermissions(auth) {
    const permissions = {
      can_monitor: auth.can_monitor === true,
      can_manage_servers: auth.can_manage_servers === true,
      is_admin: auth.is_admin === true
    };
    window.hmPermissions = permissions;
    document.body.dataset.role = auth.role || 'unknown';
    document.querySelectorAll('[data-route]').forEach(link => {
      const route = link.dataset.route;
      link.classList.toggle('auth-hidden', route === 'game-servers' ? !permissions.can_manage_servers : !permissions.can_monitor);
    });
    if (!permissions.can_monitor && permissions.can_manage_servers) {
      if (location.hash !== '#game-servers') location.hash = 'game-servers';
    }
    if (permissions.is_admin) addUserAdministration();
    window.dispatchEvent(new CustomEvent('hm-auth-ready', {detail: permissions}));
  }

  function addUserAdministration() {
    const actions = document.querySelector('.top-actions');
    if (!actions || document.getElementById('user-admin-open')) return;
    const button = document.createElement('button');
    button.id = 'user-admin-open';
    button.className = 'auth-logout';
    button.textContent = 'Gebruikers';
    button.addEventListener('click', openUserAdministration);
    actions.appendChild(button);
  }

  async function openUserAdministration() {
    let modal = document.getElementById('user-admin-modal');
    if (!modal) {
      modal = document.createElement('div');
      modal.id = 'user-admin-modal';
      modal.className = 'user-admin-backdrop';
      modal.innerHTML = `
        <section class="user-admin-card" role="dialog" aria-modal="true" aria-labelledby="user-admin-title">
          <header><div><small>BEHEER</small><h2 id="user-admin-title">Gebruikers beheren</h2></div><button type="button" id="user-admin-close" aria-label="Sluiten">×</button></header>
          <form id="user-create-form" class="user-create-form">
            <h3>Nieuwe gebruiker</h3>
            <label>Gebruikersnaam<input name="username" required minlength="3" maxlength="32" pattern="[A-Za-z0-9._-]+"></label>
            <label>Tijdelijk wachtwoord<input name="password" type="password" required minlength="12" autocomplete="new-password"></label>
            <label>Rechten<select name="role"><option value="monitor">Alleen monitoring</option><option value="servers">Alleen serverbeheer</option><option value="both">Monitoring + serverbeheer</option><option value="admin">Beheerder</option></select></label>
            <button type="submit">Gebruiker aanmaken</button><p id="user-admin-message" role="status"></p>
          </form>
          <h3>Bestaande gebruikers</h3><div id="user-admin-list">Laden…</div>
        </section>`;
      document.body.appendChild(modal);
      modal.querySelector('#user-admin-close').addEventListener('click', () => modal.classList.remove('is-open'));
      modal.addEventListener('click', e => { if (e.target === modal) modal.classList.remove('is-open'); });
      modal.querySelector('#user-create-form').addEventListener('submit', createDashboardUser);
      modal.querySelector('#user-admin-list').addEventListener('click', handleUserAction);
      modal.querySelector('#user-admin-list').addEventListener('change', handleUserRoleChange);
    }
    modal.classList.add('is-open');
    await refreshUserList();
  }

  const escapeHTML = value => String(value ?? '').replace(/[&<>"']/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[ch]));

  async function refreshUserList() {
    const list = document.getElementById('user-admin-list');
    if (!list) return;
    try {
      const response = await fetch('/api/v1/users', {credentials:'same-origin', cache:'no-store'});
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || 'Gebruikers ophalen mislukt');
      const labels = {admin:'Beheerder',monitor:'Alleen monitoring',servers:'Alleen serverbeheer',both:'Monitoring + serverbeheer'};
      list.innerHTML = '<div class="user-admin-rows">' + (data.users || []).map(user => `
        <article class="user-admin-row" data-username="${escapeHTML(user.username)}">
          <div><b>${escapeHTML(user.username)}</b><small>${user.must_change_password ? 'Wachtwoord wijzigen bij eerste login' : 'Account actief'}</small></div>
          <select aria-label="Rechten voor ${escapeHTML(user.username)}" data-role-select ${user.username === 'admin' ? 'disabled' : ''}>
            ${Object.entries(labels).map(([value,label]) => `<option value="${value}" ${user.role === value ? 'selected' : ''}>${label}</option>`).join('')}
          </select>
          ${user.username === 'admin' ? '<span class="user-protected">Vast account</span>' : '<input type="password" data-reset-password placeholder="Nieuw wachtwoord (optioneel)" minlength="12" aria-label="Nieuw wachtwoord"><button type="button" data-save-user>Opslaan</button><button type="button" class="user-delete" data-delete-user>Verwijderen</button>'}
        </article>`).join('') + '</div>';
    } catch (error) {
      list.textContent = error.message;
    }
  }

  async function createDashboardUser(event) {
    event.preventDefault();
    const form = event.currentTarget;
    const values = Object.fromEntries(new FormData(form).entries());
    const message = document.getElementById('user-admin-message');
    try {
      const response = await fetch('/api/v1/users', {method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify(values)});
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || 'Gebruiker aanmaken mislukt');
      form.reset();
      message.textContent = 'Gebruiker aangemaakt. Bij de eerste login moet het tijdelijke wachtwoord worden gewijzigd.';
      await refreshUserList();
    } catch (error) { message.textContent = error.message; }
  }

  async function handleUserAction(event) {
    const row = event.target.closest('.user-admin-row');
    if (!row) return;
    const username = row.dataset.username;
    if (event.target.matches('[data-delete-user]')) {
      if (!confirm('Gebruiker ' + username + ' verwijderen?')) return;
      const response = await fetch('/api/v1/users/' + encodeURIComponent(username), {method:'DELETE',credentials:'same-origin'});
      const data = await response.json();
      if (!response.ok) { document.getElementById('user-admin-message').textContent = data.error || 'Verwijderen mislukt'; return; }
      await refreshUserList();
    }
    if (event.target.matches('[data-save-user]')) {
      const payload = {role:row.querySelector('[data-role-select]').value,password:row.querySelector('[data-reset-password]').value};
      const response = await fetch('/api/v1/users/' + encodeURIComponent(username), {method:'PUT',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});
      const data = await response.json();
      if (!response.ok) { document.getElementById('user-admin-message').textContent = data.error || 'Opslaan mislukt'; return; }
      document.getElementById('user-admin-message').textContent = data.password_reset ? 'Rechten opgeslagen. Wachtwoord gereset; de gebruiker moet dit bij de volgende login wijzigen.' : 'Rechten opgeslagen.';
      await refreshUserList();
    }
  }

  function handleUserRoleChange(event) {
    if (event.target.matches('[data-role-select]')) event.target.closest('.user-admin-row').classList.add('user-row-dirty');
  }

  checkSession();
})();