// journalref.js: nomor referensi jurnal terisi otomatis berupa 10 digit angka
// acak dengan digit pertama 1-9 (tidak boleh 0). Fungsi: penanda pelacakan
// jurnal yang tidak berpola; kesamaan referensi antar jurnal ditolak database
// (UNIQUE per perusahaan) dan diberi pesan jelas. Isian manual pengguna
// selalu dihormati; nomor hanya dibuat bila kolom masih kosong.
(function () {
  function nomorAcak() {
    var buf = new Uint8Array(10);
    crypto.getRandomValues(buf);
    var b = [];
    b.push(String(1 + (buf[0] % 9)));
    for (var i = 1; i < 10; i++) {
      b.push(String(buf[i] % 10));
    }
    return b.join('');
  }

  function init(root) {
    var form = (root || document).querySelector('#journal-form');
    if (!form || form.hasAttribute('data-ref-ready')) return;
    form.setAttribute('data-ref-ready', '1');
    var ref = form.querySelector('#reference');
    if (!ref) return;
    var edited = ref.value.trim() !== '';
    ref.addEventListener('input', function () {
      edited = ref.value.trim() !== '';
    });
    if (!edited && ref.value.trim() === '') {
      ref.value = nomorAcak();
    }
  }

  document.addEventListener('DOMContentLoaded', function () { init(document); });
  document.addEventListener('htmx:afterSwap', function (e) { init(e.target || document); });
  if (document.readyState !== 'loading') init(document);
})();
