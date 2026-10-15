// negative.js: seluruh angka bernilai minus ditampilkan dengan huruf merah.
// Dua bentuk yang ditangani: teks yang seluruhnya berupa angka minus, dan
// angka minus yang berada di dalam kalimat (mis. keterangan grafik). Berjalan
// pada pemuatan halaman dan setelah setiap pertukaran HTMX, jadi berlaku untuk
// semua halaman tanpa mengganti satu per satu tempat render.
(function () {
  var ONLY_NUMBER = /^-\s?\d[\d.,]*$/;
  // Angka minus di dalam kalimat: didahului awal teks, spasi, kurung, titik
  // dua, atau tanda sama dengan. Kode seperti "JV-0001" tidak ikut karena
  // huruf/angka di depannya tidak termasuk awalan yang diizinkan.
  var INLINE = /(^|[\s(:,=])-\s?\d[\d.,]*/g;

  function bolehDitandai(el) {
    if (!el || el.nodeType !== 1) return false;
    var tag = el.tagName.toLowerCase();
    if (tag === 'script' || tag === 'style' || tag === 'title') return false;
    if (el.closest && el.closest('svg')) return false;
    if (el.classList && el.classList.contains('text-danger')) return false;
    return true;
  }

  function spanMerah(text) {
    var span = document.createElement('span');
    span.className = 'text-danger';
    span.textContent = text;
    return span;
  }

  function tandai(node) {
    var value = node.nodeValue;
    if (!value) return;
    var parent = node.parentNode;
    if (!bolehDitandai(parent)) return;

    if (ONLY_NUMBER.test(value.trim())) {
      parent.replaceChild(spanMerah(value), node);
      return;
    }

    INLINE.lastIndex = 0;
    if (!INLINE.test(value)) return;
    INLINE.lastIndex = 0;

    // Pecah teks: hanya angka minus yang dibungkus, sisanya tetap apa adanya.
    var frag = document.createDocumentFragment();
    var last = 0;
    var match;
    while ((match = INLINE.exec(value)) !== null) {
      var angka = match[0].slice(match[1].length);
      var startAngka = match.index + match[1].length;
      if (startAngka > last) frag.appendChild(document.createTextNode(value.slice(last, startAngka)));
      frag.appendChild(spanMerah(angka));
      last = startAngka + angka.length;
    }
    if (last === 0) return;
    if (last < value.length) frag.appendChild(document.createTextNode(value.slice(last)));
    parent.replaceChild(frag, node);
  }

  function mark(root) {
    if (!root) return;
    var walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, null, false);
    var texts = [];
    var node;
    while ((node = walker.nextNode())) texts.push(node);
    for (var i = 0; i < texts.length; i++) tandai(texts[i]);
  }

  function run() { mark(document.body); }

  document.addEventListener('DOMContentLoaded', run);
  document.addEventListener('htmx:afterSwap', function (e) { mark(e.target || document.body); });
  document.addEventListener('htmx:afterSettle', function (e) { mark(e.target || document.body); });
  if (document.readyState !== 'loading') run();
})();
