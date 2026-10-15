// password.js: pemeriksa isian formulir pendaftaran undangan.
// Menangani tiga hal: ketersediaan username (menanyakan ke server), kekuatan
// password beserta syarat karakternya, dan kecocokan kedua kolom password.
// Tombol lanjut hanya aktif bila seluruh isian sah. Aturan di sini sama dengan
// pemeriksaan server di internal/auth/password_policy.go.
(function () {
  var MIN_PANJANG = 8;
  var KUAT_PANJANG = 12;

  function periksaPassword(pw, pw2) {
    var hasil = {
      length: (pw || '').length >= MIN_PANJANG,
      upper: /[A-Z]/.test(pw || ''),
      digit: /[0-9]/.test(pw || ''),
      symbol: /[^A-Za-z0-9]/.test(pw || ''),
      match: (pw || '') !== '' && pw === pw2
    };
    var gagal = Object.keys(hasil).filter(function (k) { return !hasil[k]; });
    var skor = ['length', 'upper', 'digit', 'symbol'].filter(function (k) { return hasil[k]; }).length;
    var tingkat = gagal.length > 0 ? 'Lemah' : ((pw || '').length >= KUAT_PANJANG ? 'Sangat kuat' : 'Kuat');
    return { hasil: hasil, gagal: gagal, skor: skor, tingkat: tingkat };
  }

  function emailSah(v) {
    return v === '' || /^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(v);
  }

  function setAturan(root, hasil) {
    Object.keys(hasil).forEach(function (k) {
      var li = root.querySelector('[data-rule="' + k + '"]');
      if (!li) return;
      var tanda = li.querySelector('[data-rule-mark]');
      if (hasil[k]) {
        li.className = 'flex items-center gap-2 text-ok';
        if (tanda) tanda.textContent = '✓';
      } else {
        li.className = 'flex items-center gap-2 text-muted';
        if (tanda) tanda.textContent = '○';
      }
    });
  }

  function init(form) {
    form.setAttribute('data-ready', '1');
    var nama = form.querySelector('input[name="full_name"]');
    var username = form.querySelector('input[name="username"]');
    var email = form.querySelector('input[name="email"]');
    var pw = form.querySelector('input[name="password"]');
    var pw2 = form.querySelector('input[name="password2"]');
    var kokoh = form.querySelector('[data-password-check]');
    var cocok = form.querySelector('[data-match-check]');
    var noteUser = form.querySelector('[data-username-note]');
    var noteEmail = form.querySelector('[data-email-note]');
    var tombol = form.querySelector('[data-submit]');
    var endpoint = window.location.pathname.replace(/\/$/, '') + '/cek-username';

    var statusUser = { tersedia: false, dicek: '' };
    var timer = null;

    function perbarui() {
      var r = periksaPassword(pw.value, pw2.value);

      if (kokoh) {
        var label = kokoh.querySelector('[data-password-label]');
        var bar = kokoh.querySelector('[data-password-bar]');
        if (label) {
          label.textContent = r.tingkat;
          label.className = 'text-xs font-semibold ' + (r.tingkat === 'Lemah' ? 'text-danger' : 'text-ok');
        }
        if (bar) {
          var lebar = r.tingkat === 'Lemah' ? Math.max(10, r.skor * 20) : (r.tingkat === 'Kuat' ? 70 : 100);
          bar.style.width = lebar + '%';
          bar.className = 'h-full rounded-full transition-all duration-300 ' + (r.tingkat === 'Lemah' ? 'bg-danger' : 'bg-ok');
        }
        setAturan(kokoh, r.hasil);
      }
      if (cocok) setAturan(cocok, r.hasil);

      var emailBersih = (email ? email.value : '').trim().toLowerCase();
      var emailOk = emailSah(emailBersih);
      if (noteEmail) noteEmail.classList.toggle('hidden', emailOk);

      var namaOk = (nama.value || '').trim().length >= 2;
      var userBersih = (username.value || '').trim().toLowerCase();
      var userOk = userBersih.length >= 3 && statusUser.tersedia && statusUser.dicek === userBersih;
      var passwordOk = r.gagal.length === 0;

      if (tombol) tombol.disabled = !(namaOk && userOk && passwordOk && emailOk);
    }

    function cekUsername() {
      var v = (username.value || '').trim().toLowerCase();
      if (!noteUser) return;
      if (v.length < 3) {
        statusUser = { tersedia: false, dicek: '' };
        noteUser.className = 'mt-1.5 text-xs text-muted';
        noteUser.textContent = 'Minimal 3 karakter: huruf kecil, angka, titik, garis bawah, atau tanda hubung.';
        perbarui();
        return;
      }
      noteUser.className = 'mt-1.5 text-xs text-muted';
      noteUser.textContent = 'Memeriksa ketersediaan...';
      fetch(endpoint + '?username=' + encodeURIComponent(v), { headers: { Accept: 'application/json' } })
        .then(function (r) { return r.json(); })
        .then(function (d) {
          if ((username.value || '').trim().toLowerCase() !== v) return;
          statusUser = { tersedia: !!d.tersedia, dicek: v };
          noteUser.className = 'mt-1.5 text-xs ' + (d.tersedia ? 'text-ok' : 'text-danger');
          noteUser.textContent = d.pesan || (d.tersedia ? 'Username tersedia.' : 'Username sudah dipakai.');
          perbarui();
        })
        .catch(function () {
          statusUser = { tersedia: false, dicek: '' };
          noteUser.className = 'mt-1.5 text-xs text-danger';
          noteUser.textContent = 'Gagal memeriksa username, coba lagi.';
          perbarui();
        });
    }

    if (username) {
      username.addEventListener('input', function () {
        statusUser = { tersedia: false, dicek: '' };
        perbarui();
        clearTimeout(timer);
        timer = setTimeout(cekUsername, 400);
      });
    }
    [nama, email, pw, pw2].forEach(function (el) {
      if (!el) return;
      ['input', 'change', 'keyup'].forEach(function (ev) { el.addEventListener(ev, perbarui); });
    });
    perbarui();
    if (username && (username.value || '').trim().length >= 3) cekUsername();
  }

  function mulai(root) {
    var forms = (root || document).querySelectorAll('form[data-signup-form]:not([data-ready])');
    for (var i = 0; i < forms.length; i++) init(forms[i]);
  }

  document.addEventListener('DOMContentLoaded', function () { mulai(document); });
  if (document.readyState !== 'loading') mulai(document);
})();
