import {
  File, FileArchive, FileAudio, FileCode2, FileImage, FileJson, FileKey2, FileLock2, FileSpreadsheet, FileTerminal,
  FileText, FileType2, FileVideo, Folder, FolderSymlink, Database, Link2, Presentation, BookOpen, Cog, HardDrive, Package,
} from 'lucide-react'
import type { Entry } from './types'
import { ext } from './paths'
import { t } from './i18n'

type Cat = { icon: any; color: string; label: string }

const C = {
  folder: { icon: Folder, color: '#f2b544', label: 'Folder' },
  image: { icon: FileImage, color: '#e8618c', label: 'Image' },
  video: { icon: FileVideo, color: '#a26bf5', label: 'Video' },
  audio: { icon: FileAudio, color: '#14b8a6', label: 'Audio' },
  archive: { icon: FileArchive, color: '#f08a24', label: 'Archive' },
  code: { icon: FileCode2, color: '#4f8cff', label: 'Source code' },
  web: { icon: FileCode2, color: '#e5734a', label: 'Web document' },
  json: { icon: FileJson, color: '#e0b31a', label: 'Data' },
  text: { icon: FileText, color: '#8b93a6', label: 'Text document' },
  doc: { icon: FileText, color: '#3b82f6', label: 'Document' },
  pdf: { icon: BookOpen, color: '#e5484d', label: 'PDF document' },
  sheet: { icon: FileSpreadsheet, color: '#22a35a', label: 'Spreadsheet' },
  slides: { icon: Presentation, color: '#ea7a1c', label: 'Presentation' },
  shell: { icon: FileTerminal, color: '#3dbb79', label: 'Script' },
  key: { icon: FileKey2, color: '#d6a01c', label: 'Key / certificate' },
  lock: { icon: FileLock2, color: '#8b93a6', label: 'Lock file' },
  font: { icon: FileType2, color: '#c084fc', label: 'Font' },
  db: { icon: Database, color: '#0ea5e9', label: 'Database' },
  config: { icon: Cog, color: '#7c8aa5', label: 'Configuration' },
  disk: { icon: HardDrive, color: '#64748b', label: 'Disk image' },
  pkg: { icon: Package, color: '#b7791f', label: 'Package' },
  file: { icon: File, color: '#9aa1b1', label: 'File' },
} satisfies Record<string, Cat>

const map: Record<string, Cat> = {}
const reg = (cat: Cat, exts: string) => exts.split(' ').forEach((e) => (map[e] = cat))
reg(C.image, 'png jpg jpeg gif bmp webp svg ico tif tiff heic heif avif psd raw cr2 nef xcf')
reg(C.video, 'mp4 mkv avi mov wmv flv webm m4v mpg mpeg 3gp ts')
reg(C.audio, 'mp3 wav flac ogg aac m4a wma opus aiff mid midi')
reg(C.archive, 'zip tar gz tgz bz2 xz 7z rar zst lz4 lzma cab tbz2 txz')
reg(C.code, 'js mjs cjs jsx ts tsx go rs py rb php java kt kts c h cpp hpp cc cs swift m mm scala dart lua pl pm r jl ex exs erl hs clj vue svelte elm zig nim v sql graphql proto')
reg(C.web, 'html htm xhtml css scss sass less')
reg(C.json, 'json yaml yml toml xml csv tsv ndjson jsonl plist')
reg(C.text, 'txt md markdown rst log nfo readme license changelog adoc tex srt')
reg(C.doc, 'doc docx odt rtf pages')
reg(C.pdf, 'pdf epub')
reg(C.sheet, 'xls xlsx ods numbers')
reg(C.slides, 'ppt pptx odp key')
reg(C.shell, 'sh bash zsh fish ps1 bat cmd exe msi bin run command')
reg(C.key, 'pem key crt cer csr pub ppk p12 pfx gpg asc der')
reg(C.lock, 'lock')
reg(C.font, 'ttf otf woff woff2 eot')
reg(C.db, 'db sqlite sqlite3 mdb accdb dump bak')
reg(C.config, 'conf cfg ini env properties htaccess service cnf rc editorconfig gitignore dockerignore')
reg(C.disk, 'iso img dmg vhd vhdx vmdk qcow2')
reg(C.pkg, 'deb rpm apk pkg jar war whl gem nupkg snap flatpak appimage')

const special: Record<string, Cat> = {
  dockerfile: C.config, makefile: C.code, '.htaccess': C.config, '.env': C.config, 'readme': C.text, 'license': C.text,
  '.bashrc': C.shell, '.zshrc': C.shell, '.profile': C.shell, 'authorized_keys': C.key, 'known_hosts': C.key, 'id_rsa': C.key, 'id_ed25519': C.key,
}

export function category(e: Pick<Entry, 'name' | 'isDir'>): Cat {
  if (e.isDir) return C.folder
  const lower = e.name.toLowerCase()
  if (special[lower]) return special[lower]
  return map[ext(e.name)] ?? (lower.startsWith('.') ? map[lower.slice(1)] ?? C.config : C.file)
}

export function typeLabel(e: Entry): string {
  if (e.isLink && e.isDir) return t('ftype.linkDir')
  if (e.isLink) return t('ftype.link')
  const c = category(e)
  const x = ext(e.name)
  const key = (Object.keys(C) as (keyof typeof C)[]).find((k) => C[k] === c) || 'file'
  if (e.isDir) return t('ftype.folder')
  if (c === C.file && x) return t('ftype.ext', { ext: x.toUpperCase() })
  return t(`ftype.${key}`)
}

export function FileIcon({ entry, size = 16 }: { entry: Pick<Entry, 'name' | 'isDir' | 'isLink'>; size?: number }) {
  const c = category(entry)
  const Icon = entry.isLink && entry.isDir ? FolderSymlink : c.icon
  return (
    <span className="ficon" style={{ color: c.color, width: size, height: size }}>
      <Icon width={size} height={size} strokeWidth={entry.isDir ? 1.9 : 1.7} fill={entry.isDir ? 'currentColor' : 'none'} fillOpacity={entry.isDir ? 0.18 : 0} />
      {entry.isLink && !entry.isDir && <Link2 className="ficon-link" />}
    </span>
  )
}

const textExts = new Set('txt md markdown rst log nfo js mjs cjs jsx ts tsx go rs py rb php java kt c h cpp hpp cc cs swift sql graphql proto html htm css scss sass less json yaml yml toml xml csv tsv ini conf cfg env properties sh bash zsh fish ps1 bat cmd service htaccess gitignore dockerignore editorconfig lua pl r jl ex exs vue svelte dart scala srt tex adoc lock pem crt pub cnf rc'.split(' '))

export function isTextLike(name: string, size = 0): boolean {
  const lower = name.toLowerCase()
  if (special[lower] && special[lower] !== C.db) return true
  const x = ext(name)
  if (!x) return size < 512 * 1024
  return textExts.has(x)
}

export function previewKind(name: string): 'image' | 'video' | 'audio' | 'pdf' | 'text' | 'markdown' | null {
  const x = ext(name)
  if (['png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'ico', 'avif'].includes(x)) return 'image'
  if (['mp4', 'webm', 'mov', 'm4v', 'ogv'].includes(x)) return 'video'
  if (['mp3', 'wav', 'ogg', 'flac', 'm4a', 'aac', 'opus'].includes(x)) return 'audio'
  if (x === 'pdf') return 'pdf'
  if (x === 'md' || x === 'markdown') return 'markdown'
  if (isTextLike(name)) return 'text'
  return null
}
