// Names for the controls that only show an icon. Every such button carries
// its tooltip in data-tip: a screen reader gets it as the name of the
// button (aria-label). The choices of a segmented control say which one is
// on (aria-pressed). Done once for the whole page, as controls come and go.

const CONTROLS = 'button, a[href], [role="button"], [role="menuitem"], input, select, textarea'

function hasName(el: HTMLElement): boolean {
  if (el.getAttribute('aria-labelledby')) return true
  if (el.hasAttribute('aria-label') && !el.dataset.a11yLabel) return true
  if (el instanceof HTMLInputElement || el instanceof HTMLSelectElement || el instanceof HTMLTextAreaElement) {
    return !!el.labels?.length || (el instanceof HTMLInputElement && ['button', 'submit', 'reset'].includes(el.type) && !!el.value)
  }
  return !!el.textContent?.trim()
}

function name(el: HTMLElement) {
  // a label of ours yields to a text the control shows now
  if (el.dataset.a11yLabel && el.textContent?.trim()) {
    el.removeAttribute('aria-label')
    delete el.dataset.a11yLabel
  }
  if (hasName(el)) return
  // a form field: the label of its .field (written beside it, not tied to it)
  const field = el.matches('input, select, textarea') ? el.closest('.field')?.querySelector(':scope > label')?.textContent?.trim() : ''
  const label = el.getAttribute('data-tip') || el.getAttribute('title') || field || el.getAttribute('placeholder')
  if (label && el.getAttribute('aria-label') !== label) {
    el.setAttribute('aria-label', label)
    el.dataset.a11yLabel = '1'
  }
}

function pressed(el: HTMLElement) {
  if (el.parentElement?.classList.contains('segmented')) el.setAttribute('aria-pressed', String(el.classList.contains('on')))
}

function visit(root: ParentNode) {
  if (root instanceof HTMLElement && root.matches(CONTROLS)) {
    name(root)
    pressed(root)
  }
  root.querySelectorAll<HTMLElement>(CONTROLS).forEach((el) => {
    name(el)
    pressed(el)
  })
}

export function installA11y() {
  let pending = new Set<ParentNode>()
  let scheduled = false
  const flush = () => {
    scheduled = false
    const roots = pending
    pending = new Set()
    roots.forEach((r) => (r as Node).isConnected && visit(r))
  }
  const queue = (n: ParentNode) => {
    pending.add(n)
    if (!scheduled) {
      scheduled = true
      requestAnimationFrame(flush)
    }
  }
  new MutationObserver((records) => {
    for (const r of records) {
      if (r.type === 'childList') r.addedNodes.forEach((n) => n instanceof HTMLElement && queue(n))
      else if (r.target instanceof HTMLElement) queue(r.target)
    }
  }).observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['data-tip', 'class', 'title'] })
  visit(document.body)
}
