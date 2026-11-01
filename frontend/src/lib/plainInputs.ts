/**
 * Soliton Rudder is a tool for names, hostnames, labels and YAML: autocorrect, auto-capitalisation and spell checking
 * only get in the way (macOS even pops up "Orders ✕" under a search box). Turn them off for every text field and text
 * area when it gets focus, so no individual field has to remember to.
 */
const TEXT_TYPES = new Set(['', 'text', 'search', 'url', 'email', 'tel'])

export function plainTextAttributes(el: Element): boolean {
  const isInput = el instanceof HTMLInputElement && TEXT_TYPES.has(el.type)
  if (!isInput && !(el instanceof HTMLTextAreaElement)) return false
  el.setAttribute('autocorrect', 'off')
  el.setAttribute('autocapitalize', 'off')
  el.spellcheck = false
  return true
}

/** Installs the focus listener; returns a function that removes it. */
export function installPlainInputs(root: Document = document): () => void {
  const onFocus = (e: Event) => {
    if (e.target instanceof Element) plainTextAttributes(e.target)
  }
  root.addEventListener('focusin', onFocus, true)
  return () => root.removeEventListener('focusin', onFocus, true)
}
