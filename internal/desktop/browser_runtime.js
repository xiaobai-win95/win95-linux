/* Windows 95 Linux browser DOM compatibility layer (26.2).
   Runs before page scripts; kept small on purpose. */
(function () {
  function $(sel, root) { return (root || document).querySelector(sel); }
  function $$(sel, root) {
    var list = (root || document).querySelectorAll(sel);
    return Array.prototype.slice.call(list);
  }
  function ready(fn) {
    if (document.readyState !== "loading") fn();
    else document.addEventListener("DOMContentLoaded", fn);
  }
  function el(tag, text) {
    var n = document.createElement(tag);
    if (text !== undefined && text !== null) n.textContent = String(text);
    return n;
  }
  window.$ = window.$ || $;
  window.$$ = window.$$ || $$;
  window.ready = window.ready || ready;
  window.el = window.el || el;
  window.Win95 = {
    version: "26.2",
    engine: "goja",
    isBrowser: true
  };
})();
