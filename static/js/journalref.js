// journalref.js: nomor referensi jurnal terisi otomatis dari akun baris
// pertama: kode akun + nama akun (tanpa spasi), contoh "1-1000 Kas".
// Baru mengisi jika kolom referensi masih kosong ATAU belum pernah diedit
// manual, supaya isian milik pengguna tidak tertimpa.
(function () {
  function init(root) {
    var form = (root || document).querySelector('#journal-form');
    if (!form || form.hasAttribute('data-ref-ready')) return;
    form.setAttribute('data-ref-ready', '1');
    var ref = form.querySelector('#reference');
    if (!ref) return;
    var edited = ref.value.trim() !== '';
    ref.addEventListener('input', function () { edited = ref.value.trim() !== ''; });

    function barisPertama() {
      return form.querySelector('#journal-lines tr select[name="account_id"]');
    }
    function isiOtomatis() {
      if (edited) return;
      var sel = barisPertama();
      if (!sel || sel.value === '0' || !sel.selectedIndex) return;
      var t = (sel.options[sel.selectedIndex].textContent || '').trim();
      var i = t.indexOf(' - ');
      if (i < 0) return;
      var kode = t.slice(0, i);
      var nama = t.slice(i + 3).replace(/\s+/g, '-');
      ref.value = kode + ' ' + nama;
    }
    form.addEventListener('change', function (e) {
      if (e.target && e.target.name === 'account_id' && e.target === barisPertama()) {
        isiOtomatis();
      }
    });
    isiOtomatis();
  }

  document.addEventListener('DOMContentLoaded', function () { init(document); });
  document.addEventListener('htmx:afterSwap', function (e) { init(e.target || document); });
  if (document.readyState !== 'loading') init(document);
})();
