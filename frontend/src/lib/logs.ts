// The text of the logs followed live (logs.follow), from the events of the
// server to the tab that shows it. Text that comes before the tab knows its
// id waits here.

type Listener = { data: (text: string) => void; end: (error: string) => void; note: (kind: string) => void }

const listeners = new Map<string, Listener>()
const early = new Map<string, { text: string; end?: string; notes: string[] }>()

export function listenLogs(id: string, l: Listener) {
  listeners.set(id, l)
  const e = early.get(id)
  if (e) {
    early.delete(id)
    if (e.text) l.data(e.text)
    e.notes.forEach(l.note)
    if (e.end !== undefined) l.end(e.end)
  }
  return () => listeners.delete(id)
}

function pending(id: string) {
  let e = early.get(id)
  if (!e) {
    e = { text: '', notes: [] }
    early.set(id, e)
    setTimeout(() => early.delete(id), 30_000)
  }
  return e
}

export function onLogsEvent(type: string, data: { id: string; text?: string; error?: string; kind?: string }) {
  const l = listeners.get(data.id)
  if (type === 'logs.data') l ? l.data(data.text || '') : (pending(data.id).text += data.text || '')
  else if (type === 'logs.end') l ? l.end(data.error || '') : (pending(data.id).end = data.error || '')
  else if (type === 'logs.note') l ? l.note(data.kind || '') : pending(data.id).notes.push(data.kind || '')
}
