import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, drawSelection, dropCursor, rectangularSelection, crosshairCursor, highlightSpecialChars } from '@codemirror/view'
import { EditorState, Compartment, type Extension } from '@codemirror/state'
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { searchKeymap, highlightSelectionMatches } from '@codemirror/search'
import { autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap } from '@codemirror/autocomplete'
import { bracketMatching, foldGutter, foldKeymap, HighlightStyle, indentOnInput, indentUnit, syntaxHighlighting, LanguageDescription } from '@codemirror/language'
import { languages } from '@codemirror/language-data'
import { tags } from '@lezer/highlight'
import { getLang } from '../../lib/i18n'

export const highlight = HighlightStyle.define([
  { tag: [tags.keyword, tags.controlKeyword, tags.moduleKeyword, tags.operatorKeyword], color: 'var(--syn-kw)' },
  { tag: [tags.definitionKeyword, tags.modifier], color: 'var(--syn-kw)' },
  { tag: [tags.string, tags.special(tags.string), tags.regexp], color: 'var(--syn-str)' },
  { tag: [tags.number, tags.bool, tags.null, tags.atom], color: 'var(--syn-num)' },
  { tag: [tags.comment, tags.lineComment, tags.blockComment, tags.docComment], color: 'var(--syn-comment)', fontStyle: 'italic' },
  { tag: [tags.function(tags.variableName), tags.function(tags.propertyName)], color: 'var(--syn-fn)' },
  { tag: [tags.typeName, tags.className, tags.namespace], color: 'var(--syn-type)' },
  { tag: [tags.propertyName, tags.attributeName], color: 'var(--syn-prop)' },
  { tag: [tags.tagName], color: 'var(--syn-tag)' },
  { tag: [tags.variableName], color: 'var(--syn-var)' },
  { tag: [tags.operator, tags.punctuation, tags.bracket], color: 'var(--syn-punct)' },
  { tag: [tags.meta, tags.processingInstruction], color: 'var(--syn-meta)' },
  { tag: [tags.heading], color: 'var(--syn-kw)', fontWeight: '700' },
  { tag: [tags.link, tags.url], color: 'var(--syn-str)', textDecoration: 'underline' },
  { tag: tags.emphasis, fontStyle: 'italic' },
  { tag: tags.strong, fontWeight: '700' },
  { tag: tags.invalid, color: 'var(--danger)' },
])

export const baseTheme = EditorView.theme({
  '&': { height: '100%', backgroundColor: 'var(--panel)', color: 'var(--text)' },
  '.cm-scroller': { fontFamily: 'var(--font-mono)', lineHeight: '1.55' },
  '.cm-content': { caretColor: 'var(--accent)', padding: '8px 0' },
  '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--accent)', borderLeftWidth: '2px' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': { backgroundColor: 'var(--sel-focus) !important' },
  '.cm-gutters': { backgroundColor: 'var(--panel)', color: 'var(--faint)', border: 'none', borderRight: '1px solid var(--border)' },
  '.cm-activeLineGutter': { backgroundColor: 'transparent', color: 'var(--text-2)' },
  '.cm-activeLine': { backgroundColor: 'var(--accent-softer)' },
  '.cm-foldPlaceholder': { backgroundColor: 'var(--surface)', border: '1px solid var(--border)', color: 'var(--muted)' },
  '.cm-selectionMatch': { backgroundColor: 'var(--accent-soft)' },
  '&.cm-focused .cm-matchingBracket': { backgroundColor: 'var(--accent-soft)', outline: '1px solid var(--accent-ring)' },
  '.cm-searchMatch': { backgroundColor: 'color-mix(in srgb, var(--warn) 30%, transparent)', outline: '1px solid var(--warn)' },
  '.cm-searchMatch.cm-searchMatch-selected': { backgroundColor: 'color-mix(in srgb, var(--warn) 55%, transparent)' },
  '.cm-panels': { backgroundColor: 'var(--panel-2)', color: 'var(--text)', borderTop: '1px solid var(--border)' },
  '.cm-panels.cm-panels-top': { borderBottom: '1px solid var(--border)' },
  '.cm-panel.cm-search': { padding: '8px 10px', fontFamily: 'var(--font-ui)', fontSize: '12px' },
  '.cm-panel.cm-search input, .cm-panel.cm-search button, .cm-panel.cm-search label': { fontSize: '12px' },
  '.cm-textfield': { background: 'var(--bg-elev)', border: '1px solid var(--border-strong)', borderRadius: '5px', padding: '3px 6px', color: 'var(--text)' },
  '.cm-button': { background: 'var(--surface)', backgroundImage: 'none', border: '1px solid var(--border-strong)', borderRadius: '5px', color: 'var(--text)', padding: '2px 8px' },
  '.cm-tooltip': { backgroundColor: 'var(--bg-elev)', border: '1px solid var(--border)', borderRadius: '6px', boxShadow: 'var(--shadow)' },
  '.cm-tooltip-autocomplete > ul > li[aria-selected]': { backgroundColor: 'var(--accent-soft)', color: 'var(--text)' },
  '.cm-gutterElement': { padding: '0 8px 0 6px' },
})

export function baseExtensions(opts: { readOnly?: boolean; tabSize: number }): Extension[] {
  return [
    lineNumbers(),
    highlightActiveLineGutter(),
    highlightSpecialChars(),
    history(),
    foldGutter(),
    drawSelection(),
    dropCursor(),
    EditorState.allowMultipleSelections.of(true),
    indentOnInput(),
    syntaxHighlighting(highlight),
    bracketMatching(),
    closeBrackets(),
    autocompletion({ activateOnTyping: false }),
    rectangularSelection(),
    crosshairCursor(),
    highlightActiveLine(),
    highlightSelectionMatches(),
    EditorState.tabSize.of(opts.tabSize),
    indentUnit.of(' '.repeat(opts.tabSize)),
    keymap.of([...closeBracketsKeymap, ...defaultKeymap, ...searchKeymap, ...historyKeymap, ...foldKeymap, ...completionKeymap, indentWithTab]),
    baseTheme,
    EditorState.readOnly.of(!!opts.readOnly),
    getLang() === 'fr' ? EditorState.phrases.of(PHRASES_FR) : [],
  ]
}

// French labels for CodeMirror's built-in panels (search, go to line, folding).
const PHRASES_FR: Record<string, string> = {
  Find: 'Rechercher',
  Replace: 'Remplacer',
  next: 'suivant',
  previous: 'précédent',
  all: 'tout',
  'match case': 'respecter la casse',
  regexp: 'expression régulière',
  'by word': 'mot entier',
  replace: 'remplacer',
  'replace all': 'tout remplacer',
  close: 'fermer',
  'current match': 'occurrence courante',
  'on line': 'à la ligne',
  'replaced match on line $': 'occurrence remplacée à la ligne $',
  'replaced $ matches': '$ occurrences remplacées',
  'Go to line': 'Aller à la ligne',
  go: 'aller',
  'Folded lines': 'Lignes repliées',
  'Unfolded lines': 'Lignes dépliées',
  to: 'à',
  'folded code': 'code replié',
  unfold: 'déplier',
  'Fold line': 'Replier la ligne',
  'Unfold line': 'Déplier la ligne',
  'Selection deleted': 'Sélection supprimée',
  'Control character': 'Caractère de contrôle',
  Completions: 'Suggestions',
}

export async function languageFor(filename: string): Promise<Extension | null> {
  const lower = filename.toLowerCase()
  let desc = LanguageDescription.matchFilename(languages, filename)
  if (!desc) {
    const alias: Record<string, string> = {
      dockerfile: 'Dockerfile', makefile: 'CMake', '.htaccess': 'Properties files', '.env': 'Properties files', '.bashrc': 'Shell', '.zshrc': 'Shell', '.profile': 'Shell',
    }
    const ext = lower.split('.').pop() || ''
    const extAlias: Record<string, string> = { conf: 'Nginx', cfg: 'Properties files', service: 'Properties files', env: 'Properties files', log: '', vue: 'Vue' }
    const name = alias[lower] ?? extAlias[ext]
    if (name) desc = languages.find((l) => l.name === name) || null
  }
  if (!desc) return null
  try {
    return await desc.load()
  } catch {
    return null
  }
}

export function languageName(filename: string): string {
  return LanguageDescription.matchFilename(languages, filename)?.name || ''
}

export { Compartment, EditorState, EditorView }
