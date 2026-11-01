(() => {
  const columnKeys = ["name", "state", "type", "local", "remote", "traffic", "connections", "actions"];
  const defaultColumns = { name: true, state: true, type: false, local: true, remote: true, traffic: false, connections: false, actions: true };
  const sortKeys = ["name", "state", "type", "traffic", "connections"];
  const savedSorts = JSON.parse(localStorage.getItem("frpd.proxy-sorts") || "{}");
  localStorage.removeItem("frpd.proxy-sort");
  const savedColumns = JSON.parse(localStorage.getItem("frpd.columns") || "{}");
  const savedColumnOrder = JSON.parse(localStorage.getItem("frpd.column-order") || "[]").flatMap(column => column === "metrics" ? ["traffic", "connections"] : [column]);
  const columnOrder = [...new Set(savedColumnOrder.filter(column => columnKeys.includes(column)))];
  columnKeys.forEach(column => { if (!columnOrder.includes(column)) columnOrder.push(column); });
  const settings = {
    theme: localStorage.getItem("frpd.theme") || "default",
    language: localStorage.getItem("frpd.language") || "en",
    hideVhostPorts: localStorage.getItem("frpd.hide-vhost-ports") === "true",
    customVhostPorts: localStorage.getItem("frpd.vhost-ports"),
    hasCustomVhostPorts: localStorage.getItem("frpd.vhost-ports") !== null,
    remoteLinks: localStorage.getItem("frpd.remote-links") === "true",
    showFrpsClients: localStorage.getItem("frpd.show-frps-clients") === "true",
    hideOfflineFrpsProxies: localStorage.getItem("frpd.hide-offline-frps-proxies") !== "false",
    columns: { ...defaultColumns, ...savedColumns, traffic: savedColumns.traffic ?? savedColumns.metrics ?? false, connections: savedColumns.connections ?? savedColumns.metrics ?? false },
    columnOrder,
  };
  const translations = {
    en: { settings:"Settings", dashboard:"Dashboard", addProxy:"Add proxy", state:"State", type:"Type", local:"Local", remote:"Remote", actions:"Actions", theme:"Theme", language:"Language", hideRemotePort:"Hide remote port", hideRemotePortHelp:"Hide explicit ports such as :8080 in remote addresses.", displayColumns:"Proxy list columns", showName:"Name", showState:"State", showType:"Type", showMetrics:"Show proxy metrics", showFrpsClients:"Show unmatched FRPS clients", hideOfflineFrpsProxies:"Hide offline FRPS clients and proxies", sortBy:"Sort by", sortAscending:"Sort ascending", sortDescending:"Sort descending", default:"Default", dracula:"Dracula", mocha:"Catppuccin Mocha", english:"English", norwegian:"Norwegian", german:"German" },
    no: { settings:"Innstillinger", dashboard:"Oversikt", addProxy:"Legg til proxy", state:"Status", type:"Type", local:"Lokal", remote:"Ekstern", actions:"Handlinger", theme:"Tema", language:"Språk", hideRemotePort:"Skjul ekstern port", hideRemotePortHelp:"Skjul eksplisitte porter som :8080 i eksterne adresser.", displayColumns:"Kolonner i proxy-listen", showName:"Navn", showState:"Status", showType:"Type", showMetrics:"Vis proxy-målinger", showFrpsClients:"Vis FRPS-klienter uten tilknytning", hideOfflineFrpsProxies:"Skjul frakoblede FRPS-klienter og -proxyer", sortBy:"Sorter etter", sortAscending:"Sorter stigende", sortDescending:"Sorter synkende", default:"Standard", dracula:"Dracula", mocha:"Catppuccin Mocha", english:"Engelsk", norwegian:"Norsk", german:"Tysk" },
    de: { settings:"Einstellungen", dashboard:"Übersicht", addProxy:"Proxy hinzufügen", state:"Status", type:"Typ", local:"Lokal", remote:"Remote", actions:"Aktionen", theme:"Thema", language:"Sprache", hideRemotePort:"Remote-Port ausblenden", hideRemotePortHelp:"Explizite Ports wie :8080 in Remote-Adressen ausblenden.", displayColumns:"Proxy-Listen-Spalten", showName:"Name", showState:"Status", showType:"Typ", showMetrics:"Proxy-Metriken anzeigen", showFrpsClients:"Nicht zugeordnete FRPS-Clients anzeigen", hideOfflineFrpsProxies:"Offline-FRPS-Clients und -Proxys ausblenden", sortBy:"Sortieren nach", sortAscending:"Aufsteigend sortieren", sortDescending:"Absteigend sortieren", default:"Standard", dracula:"Dracula", mocha:"Catppuccin Mocha", english:"Englisch", norwegian:"Norwegisch", german:"Deutsch" },
  };
  const themeNames = { alucard:"Alucard", latte:"Catppuccin Latte", frappe:"Catppuccin Frappé", macchiato:"Catppuccin Macchiato", gruvbox:"Gruvbox", solarizedLight:"Solarized Light", solarizedDark:"Solarized Dark", nord:"Nord", tokyoNight:"Tokyo Night", light:"Light", softDawn:"Soft Dawn", remoteLinks:"Make web remotes clickable", remoteLinksHelp:"Open HTTP and HTTPS proxy remotes in a new tab.", hideVhostPorts:"Hide vhost ports", hideVhostPortsHelp:"Hide HTTP and HTTPS virtual-host ports in displayed remote addresses.", vhostPorts:"Vhost ports" };
  const text = key => (translations[settings.language] || translations.en)[key] || translations.en[key] || themeNames[key] || key;
  const parsePorts = value => [...new Set((value || "").split(",").map(port => Number(port.trim())).filter(port => Number.isInteger(port) && port > 0 && port <= 65535))];
  const hideVhostPort = (value, ports) => value.replace(/((?:https?:\/\/)?(?:\[[^\]]+\]|[^/,\s:]+)):(\d+)(?=\/|,|$)/g, (match, host, port) => ports.includes(Number(port)) ? host : match);
  const remoteURL = (remote, type) => /^[a-z][a-z\d+.-]*:\/\//i.test(remote) ? remote : `${type}://${remote}`;
  const reorder = () => {
    document.querySelectorAll("[data-column-options]").forEach(options => {
      settings.columnOrder.forEach(column => options.append(options.querySelector(`[data-column-option="${column}"]`)));
      options.querySelectorAll("[data-column-option]").forEach(option => {
        const index = settings.columnOrder.indexOf(option.dataset.columnOption);
        option.querySelector('[data-column-move="up"]').disabled = index === 0;
        option.querySelector('[data-column-move="down"]').disabled = index === settings.columnOrder.length - 1;
      });
    });
    document.querySelectorAll("[data-proxy-table]").forEach(table => {
      table.querySelectorAll("thead tr, tbody tr").forEach(row => {
        const cells = [...row.querySelectorAll("[data-column]")];
        settings.columnOrder.forEach(column => cells.filter(cell => cell.dataset.column === column).forEach(cell => row.append(cell)));
      });
    });
  };
  const sortColumn = header => header.dataset.sortColumn || header.dataset.column;
  const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });
  const sortScope = table => {
    const client = table.closest(".frps-client");
    if (client) {
      return JSON.stringify(["frps", table.closest("[data-frps-server]").dataset.frpsServer, client.dataset.frpsUser, client.dataset.frpsClient]);
    }
    return JSON.stringify(["instance", table.closest("[data-instance]").dataset.instance]);
  };
  const sortFor = table => {
    const saved = savedSorts[sortScope(table)] || {};
    return { column: sortKeys.includes(saved.column) ? saved.column : "name", direction: saved.direction === "desc" ? "desc" : "asc" };
  };
  const sortRows = table => {
    const headers = [...table.querySelectorAll("thead th")];
    const sort = sortFor(table);
    const active = headers.find(header => sortColumn(header) === sort.column) || headers.find(header => sortColumn(header) === "name");
    if (!active) return;
    const column = sortColumn(active);
    const direction = column === sort.column ? sort.direction : "asc";
    headers.forEach(header => {
      if (header === active) header.setAttribute("aria-sort", direction === "asc" ? "ascending" : "descending");
      else header.removeAttribute("aria-sort");
    });
    const menu = table.querySelector(".proxy-sort-mobile");
    if (menu) {
      const label = active.querySelector(".proxy-sort-button")?.textContent.trim() || active.textContent.trim();
      menu.querySelector("[data-sort-current]").textContent = `${label} ${direction === "asc" ? "↑" : "↓"}`;
      menu.querySelector("summary").setAttribute("aria-label", `${text("sortBy")}: ${label}, ${text(direction === "asc" ? "sortAscending" : "sortDescending")}`);
      menu.querySelectorAll("[data-sort-option]").forEach(button => {
        const header = headers.find(item => sortColumn(item) === button.dataset.sortOption);
        button.textContent = header?.textContent.trim() || button.dataset.sortOption;
        button.setAttribute("aria-pressed", String(button.dataset.sortOption === column));
      });
      const reverse = menu.querySelector("[data-sort-direction]");
      reverse.textContent = text(direction === "asc" ? "sortDescending" : "sortAscending");
    }
    const body = table.querySelector("tbody");
    if (!body || body.rows.length < 2) return;
    const numeric = column === "traffic" || column === "connections";
    const nameIndex = headers.find(header => sortColumn(header) === "name")?.cellIndex;
    const rows = [...body.rows].map((row, index) => {
      const cell = row.cells[active.cellIndex];
      return {
        row, index,
        value: numeric ? (cell?.dataset.sortValue === undefined ? null : BigInt(cell.dataset.sortValue)) : cell?.textContent.trim() || "",
        name: row.cells[nameIndex]?.textContent.trim() || "",
      };
    });
    rows.sort((a, b) => {
      if (a.value === null || b.value === null) {
        if (a.value !== b.value) return a.value === null ? 1 : -1;
      } else {
        const order = numeric ? (a.value < b.value ? -1 : a.value > b.value ? 1 : 0) : collator.compare(a.value, b.value);
        if (order) return direction === "asc" ? order : -order;
      }
      return collator.compare(a.name, b.name) || a.index - b.index;
    });
    body.append(...rows.map(item => item.row));
  };
  const setSort = (table, column, direction) => {
    savedSorts[sortScope(table)] = { column, direction };
    localStorage.setItem("frpd.proxy-sorts", JSON.stringify(savedSorts));
    sortRows(table);
  };
  const initSort = root => {
    root.querySelectorAll("[data-proxy-table]").forEach(table => {
      const headers = [...table.querySelectorAll("thead th")].filter(header => sortKeys.includes(sortColumn(header)));
      if (!headers.length) return;
      headers.forEach(header => {
        const column = sortColumn(header);
        const button = document.createElement("button");
        button.type = "button";
        button.className = "proxy-sort-button";
        button.textContent = header.textContent;
        if (header.dataset.i18n) {
          button.dataset.i18n = header.dataset.i18n;
          delete header.dataset.i18n;
        }
        button.addEventListener("click", () => {
          const sort = sortFor(table);
          setSort(table, column, sort.column === column && sort.direction === "asc" ? "desc" : "asc");
        });
        header.replaceChildren(button);
      });
      const controls = document.createElement("details");
      controls.className = "proxy-sort-mobile";
      const summary = document.createElement("summary");
      const current = document.createElement("span");
      current.dataset.sortCurrent = "";
      summary.append(current, document.createTextNode(" ▾"));
      const options = document.createElement("div");
      options.className = "proxy-sort-options";
      headers.forEach(header => {
        const button = document.createElement("button");
        button.type = "button";
        button.dataset.sortOption = sortColumn(header);
        button.addEventListener("click", () => {
          setSort(table, button.dataset.sortOption, "asc");
          controls.open = false;
          summary.focus();
        });
        options.append(button);
      });
      const reverse = document.createElement("button");
      reverse.type = "button";
      reverse.dataset.sortDirection = "";
      reverse.addEventListener("click", () => {
        const sort = sortFor(table);
        setSort(table, sort.column, sort.direction === "asc" ? "desc" : "asc");
        controls.open = false;
        summary.focus();
      });
      options.append(reverse);
      controls.append(summary, options);
      controls.addEventListener("keydown", event => {
        if (event.key === "Escape" && controls.open) {
          controls.open = false;
          summary.focus();
        }
      });
      controls.addEventListener("toggle", () => {
        const panel = controls.closest(".panel");
        panel?.classList.toggle("proxy-sort-open", !!panel.querySelector(".proxy-sort-mobile[open]"));
      });
      document.addEventListener("pointerdown", event => {
        if (controls.open && !controls.contains(event.target)) controls.open = false;
      });
      table.prepend(controls);
      sortRows(table);
    });
  };
  const render = () => {
    document.documentElement.dataset.theme = settings.theme;
    document.documentElement.lang = settings.language;
    document.querySelectorAll("[data-i18n]").forEach(node => { node.textContent = text(node.dataset.i18n); });
    document.querySelectorAll("[data-i18n-title]").forEach(node => { node.title = text(node.dataset.i18nTitle); });
    document.querySelectorAll("[data-remote]").forEach(node => {
      const row = node.closest("tr");
      const rawRemote = node.dataset.remote;
      const webType = row?.dataset.proxyType;
      const vhostPort = Number(webType === "https" ? row.dataset.vhostHttpsPort : row.dataset.vhostHttpPort);
      const ports = settings.hasCustomVhostPorts ? parsePorts(settings.customVhostPorts) : [vhostPort].filter(port => port > 0);
      const remote = settings.hideVhostPorts && ["http", "https"].includes(webType) ? hideVhostPort(rawRemote, ports) : rawRemote;
      node.textContent = remote;
      const discovered = row?.hasAttribute("data-frps-offline");
      const webProxy = ["http", "https"].includes(webType) && (discovered ? row.dataset.frpsOffline === "false" : row?.dataset.proxyEnabled === "true" && row?.dataset.proxyState === "running" && row?.dataset.proxyStateClass === "ok");
      const linkableRemote = !discovered || /^[a-z\d-]+(?:\.[a-z\d-]+)*(?::\d+)?$/i.test(rawRemote);
      if (settings.remoteLinks && webProxy && rawRemote && !rawRemote.includes(",") && linkableRemote) {
        node.href = remoteURL(remote, webType);
        node.target = "_blank";
        node.rel = "noreferrer";
        node.classList.add("remote-link");
      } else {
        node.removeAttribute("href");
        node.removeAttribute("target");
        node.removeAttribute("rel");
        node.classList.remove("remote-link");
      }
    });
    document.querySelectorAll("[data-column]").forEach(node => { node.classList.toggle("column-hidden", !settings.columns[node.dataset.column]); });
    document.querySelectorAll('[data-frps-offline="true"]').forEach(row => { row.hidden = settings.hideOfflineFrpsProxies; });
    document.querySelectorAll(".frps-client").forEach(client => {
      client.hidden = settings.hideOfflineFrpsProxies && client.dataset.frpsClientOnline === "false";
      const rows = [...client.querySelectorAll("[data-frps-offline]")];
      const empty = client.querySelector("[data-frps-empty]");
      if (rows.length && empty) empty.hidden = rows.some(row => !row.hidden);
    });
    document.querySelectorAll("[data-frps-server]").forEach(server => {
      const clients = [...server.querySelectorAll(".frps-client")];
      const visible = clients.filter(client => !client.hidden).length;
      server.querySelector("[data-frps-client-count]").textContent = String(visible);
      const empty = server.querySelector("[data-frps-empty-clients]");
      if (empty) empty.hidden = visible !== 0;
    });
    reorder();
    document.querySelectorAll("[data-proxy-table]").forEach(sortRows);
  };
  const persistColumns = () => localStorage.setItem("frpd.columns", JSON.stringify(settings.columns));
  const persistColumnOrder = () => localStorage.setItem("frpd.column-order", JSON.stringify(settings.columnOrder));
  const moveColumn = (column, target) => {
    const current = settings.columnOrder.indexOf(column);
    if (target < 0 || target >= settings.columnOrder.length) return;
    [settings.columnOrder[current], settings.columnOrder[target]] = [settings.columnOrder[target], settings.columnOrder[current]];
    persistColumnOrder();
    render();
  };
  document.addEventListener("DOMContentLoaded", () => {
    render();
    initSort(document);
    const theme = document.querySelector("#theme");
    const language = document.querySelector("#language");
    const hideVhostPorts = document.querySelector("#hide-vhost-ports");
    const vhostPorts = document.querySelector("#vhost-ports");
    const remoteLinks = document.querySelector("#remote-links");
    const showFrpsClients = document.querySelector("#show-frps-clients");
    const hideOfflineFrpsProxies = document.querySelector("#hide-offline-frps-proxies");
    if (theme) { theme.value = settings.theme; theme.addEventListener("change", () => { settings.theme = theme.value; localStorage.setItem("frpd.theme", settings.theme); render(); }); }
    if (language) { language.value = settings.language; language.addEventListener("change", () => { settings.language = language.value; localStorage.setItem("frpd.language", settings.language); render(); }); }
    if (hideVhostPorts) { hideVhostPorts.checked = settings.hideVhostPorts; hideVhostPorts.addEventListener("change", () => { settings.hideVhostPorts = hideVhostPorts.checked; localStorage.setItem("frpd.hide-vhost-ports", String(settings.hideVhostPorts)); render(); }); }
    if (vhostPorts) { vhostPorts.value = settings.hasCustomVhostPorts ? settings.customVhostPorts : vhostPorts.value; vhostPorts.addEventListener("input", () => { settings.customVhostPorts = vhostPorts.value; settings.hasCustomVhostPorts = true; localStorage.setItem("frpd.vhost-ports", settings.customVhostPorts); render(); }); }
    if (remoteLinks) { remoteLinks.checked = settings.remoteLinks; remoteLinks.addEventListener("change", () => { settings.remoteLinks = remoteLinks.checked; localStorage.setItem("frpd.remote-links", String(settings.remoteLinks)); render(); }); }
    if (showFrpsClients) { showFrpsClients.checked = settings.showFrpsClients; showFrpsClients.addEventListener("change", () => { settings.showFrpsClients = showFrpsClients.checked; localStorage.setItem("frpd.show-frps-clients", String(settings.showFrpsClients)); }); }
    if (hideOfflineFrpsProxies) { hideOfflineFrpsProxies.checked = settings.hideOfflineFrpsProxies; hideOfflineFrpsProxies.addEventListener("change", () => { settings.hideOfflineFrpsProxies = hideOfflineFrpsProxies.checked; localStorage.setItem("frpd.hide-offline-frps-proxies", String(settings.hideOfflineFrpsProxies)); }); }
    const initFrpsCollapse = () => {
      document.querySelectorAll("[data-frps-server]").forEach(panel => {
        const button = panel.querySelector("[data-frps-collapse]");
        const title = panel.querySelector("[data-frps-collapse-title]");
        const clients = panel.querySelector("[data-frps-client-list]");
        const key = `frpd.frps-collapsed.${panel.dataset.frpsServer}`;
        const setCollapsed = collapsed => {
          clients.hidden = collapsed;
          button.setAttribute("aria-expanded", String(!collapsed));
          title.setAttribute("aria-expanded", String(!collapsed));
          button.textContent = collapsed ? "Show" : "Hide";
        };
        setCollapsed(localStorage.getItem(key) === "true");
        const toggle = () => {
          const collapsed = button.getAttribute("aria-expanded") === "true";
          setCollapsed(collapsed);
          localStorage.setItem(key, String(collapsed));
        };
        button.addEventListener("click", toggle);
        title.addEventListener("click", toggle);
      });
    };
    const discovery = document.querySelector("[data-frps-discovery]");
    if (discovery && settings.showFrpsClients) {
      discovery.hidden = false;
      discovery.textContent = "Loading FRPS clients…";
      fetch("/frps/discovery").then(async response => {
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        discovery.innerHTML = await response.text();
        initFrpsCollapse();
        render();
        initSort(discovery);
      }).catch(error => { discovery.textContent = `Could not load FRPS clients: ${error.message}`; });
    }
    document.addEventListener("submit", event => {
      if (event.target.closest("[data-frps-prune]") && !window.confirm("Are you sure you want to clear all offline proxies?")) {
        event.preventDefault();
        return;
      }
      const message = event.target.closest("[data-confirm]")?.dataset.confirm;
      if (message && !window.confirm(message)) event.preventDefault();
    });
    document.querySelectorAll("[data-column-toggle]").forEach(input => { input.checked = settings.columns[input.dataset.columnToggle]; input.addEventListener("change", () => { settings.columns[input.dataset.columnToggle] = input.checked; persistColumns(); render(); }); });
    document.querySelectorAll("[data-column-move]").forEach(button => { button.addEventListener("click", () => { const column = button.closest("[data-column-option]").dataset.columnOption; moveColumn(column, settings.columnOrder.indexOf(column) + (button.dataset.columnMove === "up" ? -1 : 1)); }); });
    document.querySelectorAll("[data-column-drag-handle]").forEach(handle => {
      handle.addEventListener("pointerdown", event => {
        if (event.button !== 0) return;
        event.preventDefault();
        const option = handle.closest("[data-column-option]");
        const options = option.parentElement;
        const startY = event.clientY;
        let dragging = false;
        handle.setPointerCapture(event.pointerId);
        const move = pointer => {
          if (pointer.pointerId !== event.pointerId) return;
          pointer.preventDefault();
          if (!dragging && Math.abs(pointer.clientY - startY) < 6) return;
          dragging = true;
          option.classList.add("dragging");
          const target = [...options.children].filter(item => item !== option).find(item => pointer.clientY < item.getBoundingClientRect().top + item.getBoundingClientRect().height / 2);
          options.insertBefore(option, target || null);
        };
        const finish = pointer => {
          if (pointer.pointerId !== event.pointerId) return;
          window.removeEventListener("pointermove", move);
          window.removeEventListener("pointerup", finish);
          window.removeEventListener("pointercancel", finish);
          option.classList.remove("dragging");
          if (!dragging) return;
          settings.columnOrder = [...options.children].map(item => item.dataset.columnOption);
          persistColumnOrder();
          render();
        };
        window.addEventListener("pointermove", move, { passive:false });
        window.addEventListener("pointerup", finish);
        window.addEventListener("pointercancel", finish);
      });
      handle.addEventListener("dragstart", event => event.preventDefault());
    });
    document.querySelectorAll(".tunnel-panel").forEach(panel => {
      const button = panel.querySelector("[data-proxy-collapse]");
      const title = panel.querySelector("[data-proxy-collapse-title]");
      const table = panel.querySelector("[data-proxy-table]");
      const key = `frpd.tunnel-collapsed.${panel.dataset.instance}`;
      const setCollapsed = collapsed => {
        table?.classList.toggle("proxy-table-collapsed", collapsed);
        button.setAttribute("aria-expanded", String(!collapsed));
        title.setAttribute("aria-expanded", String(!collapsed));
        button.textContent = collapsed ? "Show" : "Hide";
      };
      setCollapsed(localStorage.getItem(key) === "true");
      const toggle = () => {
        const collapsed = button.getAttribute("aria-expanded") === "true";
        setCollapsed(collapsed);
        localStorage.setItem(key, String(collapsed));
      };
      button.addEventListener("click", toggle);
      title.addEventListener("click", toggle);
    });
  });
})();
