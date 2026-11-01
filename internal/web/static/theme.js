(function () {
  document.documentElement.dataset.theme = localStorage.getItem('frpd.theme') || 'default';
  document.documentElement.lang = localStorage.getItem('frpd.language') || 'en';
})();
