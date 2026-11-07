// The parts of paddock's UI that need script: <p-icon>, <p-tabs>, menus under their buttons, toast() and confirm().
// Light DOM only, so paddock.css and a page's own CSS reach inside. paddock.js imports this; pages needn't.

customElements.define('p-icon', class extends HTMLElement {
  static observedAttributes = ['name'];
  attributeChangedCallback() {
    this.style.setProperty('--icon', `url("/_paddock/icons/${encodeURIComponent(this.getAttribute('name'))}.svg")`);
    this.setAttribute('aria-hidden', 'true');
  }
});

// <p-spinner> and <p-spinner mini>: Zeron's loaders. Each cell runs the same 0.75 s flare, started part way in:
// the 3×3 one's pulse travels up and the top middle comes last, the 2×3 one's goes clockwise from the top left.
const RING = [0, 1, 5, 2, 4, 3];
customElements.define('p-spinner', class extends HTMLElement {
  connectedCallback() {
    if (this.childElementCount) return;
    const mini = this.hasAttribute('mini');
    const cells = Array.from({ length: mini ? 6 : 9 }, (_, i) => {
      const phase = mini ? RING[i] / 6 : (3 - Math.floor(i / 3) + Math.abs((i % 3) - 1)) / 4;
      const cell = document.createElement('i');
      cell.style.animationDelay = `${(-phase * 0.75).toFixed(3)}s`;
      return cell;
    });
    this.replaceChildren(...cells);
    this.setAttribute('role', 'progressbar');
    this.setAttribute('aria-label', 'Loading');
  }
});

// A button with aria-busy="true" shows a mini spinner before its label until the attribute goes.
function busy(button) {
  if (!(button instanceof HTMLButtonElement)) return;
  const on = button.getAttribute('aria-busy') === 'true';
  const spinner = button.querySelector(':scope > p-spinner[data-busy]');
  if (on && !spinner) {
    const s = document.createElement('p-spinner');
    s.setAttribute('mini', '');
    s.dataset.busy = '';
    button.prepend(s);
  } else if (!on) spinner?.remove();
}
new MutationObserver((changes) => changes.forEach((c) => busy(c.target)))
  .observe(document, { subtree: true, attributeFilter: ['aria-busy'] });
// A module runs once the page is parsed: buttons busy from the start.
document.querySelectorAll('button[aria-busy="true"]').forEach(busy);

// <p-tabs><nav><button>A</button><button>B</button></nav><section>…</section><section>…</section></p-tabs>
// The selected="<index>" attribute picks the tab; picking one fires `change` with that index in detail.
customElements.define('p-tabs', class extends HTMLElement {
  connectedCallback() {
    // ponytail: tabs and sections are read once; a page that adds tabs later makes a new <p-tabs>.
    const nav = this.querySelector(':scope > nav');
    this.tabs = [...nav.children];
    this.panels = [...this.querySelectorAll(':scope > section')];
    nav.setAttribute('role', 'tablist');
    this.tabs.forEach((tab, i) => {
      tab.type = 'button';
      tab.setAttribute('role', 'tab');
      this.panels[i]?.setAttribute('role', 'tabpanel');
      tab.onclick = () => this.select(i, true);
      // Roving focus: one tab in the Tab order, arrows and Home/End move between them.
      tab.onkeydown = (e) => {
        const n = this.tabs.length;
        const to = { ArrowRight: (i + 1) % n, ArrowLeft: (i - 1 + n) % n, Home: 0, End: n - 1 }[e.key];
        if (to === undefined) return;
        e.preventDefault();
        this.select(to, true, true);
      };
    });
    this.select(+this.getAttribute('selected') || 0);
  }
  select(index, fire = false, focus = false) {
    this.tabs.forEach((tab, i) => {
      tab.setAttribute('aria-selected', i === index);
      tab.tabIndex = i === index ? 0 : -1;
      if (this.panels[i]) this.panels[i].hidden = i !== index;
    });
    if (focus) this.tabs[index].focus();
    this.setAttribute('selected', index);
    if (fire) this.dispatchEvent(new CustomEvent('change', { detail: index }));
  }
});

// Menus: a <menu popover> opens under the button that opened it (flipping to stay on screen) with its first item
// focused; arrows and Home/End move, Enter or a click picks and closes, Esc closes (the popover's own), and focus goes
// back to the button. ponytail: positioned by script; CSS anchor positioning once every Shepherd webview has it.
const opener = (menu) => document.querySelector(`[popovertarget="${CSS.escape(menu.id)}"]`);
const items = (menu) => [...menu.querySelectorAll(':scope > button:not(:disabled)')];
document.addEventListener('toggle', (e) => {
  const menu = e.target;
  if (!(menu instanceof HTMLMenuElement)) return;
  const button = opener(menu);
  if (e.newState === 'closed') {
    if (!document.activeElement || document.activeElement === document.body || menu.contains(document.activeElement)) button?.focus();
    return;
  }
  menu.setAttribute('role', 'menu');
  items(menu).forEach((item) => { item.setAttribute('role', 'menuitem'); item.tabIndex = -1; });
  if (button) {
    const b = button.getBoundingClientRect();
    const m = menu.getBoundingClientRect();
    menu.style.left = Math.max(8, b.left + m.width > innerWidth - 8 ? b.right - m.width : b.left) + 'px';
    menu.style.top = (b.bottom + 4 + m.height > innerHeight - 8 ? b.top - 4 - m.height : b.bottom + 4) + 'px';
  }
  items(menu)[0]?.focus();
}, true);
document.addEventListener('keydown', (e) => {
  const menu = e.target.closest?.('menu[popover]');
  if (!menu) return;
  const list = items(menu);
  const i = list.indexOf(document.activeElement);
  const to = { ArrowDown: (i + 1) % list.length, ArrowUp: (i - 1 + list.length) % list.length, Home: 0, End: list.length - 1 }[e.key];
  if (to !== undefined) {
    e.preventDefault();
    list[to]?.focus();
  } else if (e.key === 'Tab') menu.hidePopover();
});
document.addEventListener('click', (e) => {
  const item = e.target.closest('menu[popover] > button');
  if (item) item.parentElement.hidePopover();
});

// toast('Saved') or toast('No such file', { kind: 'error' }): a note in the corner that goes by itself.
// kind: 'info' (default), 'success' or 'error'; timeout in ms, 0 to stay until clicked.
let stack;
export function toast(message, { kind = 'info', timeout = 4000 } = {}) {
  if (!stack) {
    stack = document.createElement('div');
    stack.className = 'p-toasts';
    stack.popover = 'manual';
    stack.setAttribute('role', 'status');
    document.body.append(stack);
  }
  const el = document.createElement('div');
  el.className = 'p-toast ' + kind;
  const icon = document.createElement('p-icon');
  icon.setAttribute('name', { success: 'check-circle', error: 'danger-circle' }[kind] ?? 'info-circle');
  const text = document.createElement('span');
  text.textContent = message;
  el.append(icon, text);
  const close = () => {
    el.remove();
    if (!stack.children.length) stack.hidePopover();
  };
  el.onclick = close;
  if (timeout) setTimeout(close, timeout);
  stack.append(el);
  // Re-shown each time, so it lands above a dialog opened since.
  if (stack.matches(':popover-open')) stack.hidePopover();
  stack.showPopover();
}

// if (await confirm('Delete 3 notes?', { ok: 'Delete', danger: true })) …
export function confirm(message, { title = '', ok = 'OK', cancel = 'Cancel', danger = false } = {}) {
  return new Promise((resolve) => {
    const dialog = document.createElement('dialog');
    if (title) dialog.append(Object.assign(document.createElement('h2'), { textContent: title }));
    const footer = document.createElement('footer');
    const no = Object.assign(document.createElement('button'), { className: 'secondary', textContent: cancel });
    const yes = Object.assign(document.createElement('button'), { className: danger ? 'danger' : '', textContent: ok });
    no.onclick = () => dialog.close('');
    yes.onclick = () => dialog.close('ok');
    footer.append(no, yes);
    dialog.append(Object.assign(document.createElement('p'), { textContent: message }), footer);
    dialog.onclose = () => {
      dialog.remove();
      resolve(dialog.returnValue === 'ok');
    };
    document.body.append(dialog);
    dialog.showModal();
    yes.focus();
  });
}
