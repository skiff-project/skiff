import { useEffect, useState } from 'react'
import { Code2, Copy } from 'lucide-react'
import { Dialog } from '../ui/Dialog'
import { openDialog } from '../../lib/dialogs'
import { call } from '../../lib/api'
import { t } from '../../lib/i18n'
import type { SiteView } from '../../lib/types'
import { copyToClipboard } from '../../lib/fileops'

export function openGenerateDialog(site: SiteView) {
  openDialog((close) => <GenerateDialog site={site} close={close} />)
}

function q(s: string) {
  return /[\s"']/.test(s) ? `"${s.replace(/"/g, '\\"')}"` : s
}

function GenerateDialog({ site, close }: { site: SiteView; close: () => void }) {
  const [withPw, setWithPw] = useState(false)
  const [url, setUrl] = useState('')
  useEffect(() => {
    call<string>('sites.url', { id: site.id, withPassword: withPw }).then(setUrl).catch(() => setUrl(''))
  }, [withPw])
  const hostkey = site.hostKey ? ` -hostkey=${q(site.hostKey)}` : ''
  const remote = site.remoteDir || '/'
  const script = `# skiff run sync.txt\nopen ${url}${hostkey}\ncd ${remote}\nlcd ${site.localDir || '.'}\nsynchronize remote -criteria=time -filemask="| .git/"\nexit`
  const cli = `skiff run -c "open ${url}${hostkey}" -c "ls ${remote}" -c exit`
  const winscp = `skiff /command "open ${url}${hostkey}" "get ${remote.replace(/\/$/, '')}/*" "exit"`
  const ssh = site.protocol === 'sftp' || site.protocol === 'scp'
    ? `ssh ${site.port && site.port !== 22 ? `-p ${site.port} ` : ''}${site.keyFile && !site.keyFile.includes('\n') ? `-i ${q(site.keyFile)} ` : ''}${(site.jumps || []).length ? `-J ${(site.jumps || []).map((j) => `${j.user ? j.user + '@' : ''}${j.host}${j.port && j.port !== 22 ? ':' + j.port : ''}`).join(',')} ` : ''}${site.user ? site.user + '@' : ''}${site.host}`
    : ''
  const block = (label: string, text: string) => (
    <div className="field">
      <label className="row" style={{ justifyContent: 'space-between' }}>
        <span>{label}</span>
        <button className="btn sm ghost" onClick={() => copyToClipboard(text)}><Copy /> {t('common.copy')}</button>
      </label>
      <pre className="code-block" style={{ margin: 0 }}>{text}</pre>
    </div>
  )
  return (
    <Dialog size="lg" title={t('gen.title')} icon={<Code2 />} onClose={close} footer={<><label className="check"><input type="checkbox" checked={withPw} onChange={(e) => setWithPw(e.target.checked)} /> {t('gen.withPassword')}</label><div className="spacer" /><button className="btn primary" onClick={close}>{t('common.close')}</button></>}>
      <div className="form">
        {block(t('gen.url'), url)}
        {block(t('gen.cli'), cli)}
        {block(t('gen.script'), script)}
        {block(t('gen.winscp'), winscp)}
        {ssh && block(t('gen.ssh'), ssh)}
      </div>
    </Dialog>
  )
}
