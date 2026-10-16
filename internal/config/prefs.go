package config

import "encoding/json"

type TransferPrefs struct {
	Mode             string `json:"mode"` // binary, text, auto
	TextMask         string `json:"textMask"`
	PreserveTime     bool   `json:"preserveTime"`
	PreservePerms    bool   `json:"preservePerms"` // copy source permissions
	SetPerms         bool   `json:"setPerms"`      // apply Perms on upload
	Perms            uint32 `json:"perms"`
	AddXToDirs       bool   `json:"addXToDirs"`
	Overwrite        string `json:"overwrite"`       // ask, overwrite, skip, newer, resume, rename, differentSize
	ResumeThreshold  int64  `json:"resumeThreshold"` // bytes; transfer via ".filepart" above it, -1 never
	FileMask         string `json:"fileMask"`
	SpeedLimit       int64  `json:"speedLimit"` // KiB/s, 0 = unlimited
	CaseMode         string `json:"caseMode"`   // none, lower, upper, firstUpper, lowerSFN
	Verify           bool   `json:"verify"`     // compare checksums after transfer
	ExcludeHidden    bool   `json:"excludeHidden"`
	ReplaceInvalid   bool   `json:"replaceInvalid"` // replace characters invalid on the target OS
	IgnorePermErrors bool   `json:"ignorePermErrors"`
	ClearArchive     bool   `json:"clearArchive"`
}

type QueuePrefs struct {
	MaxParallel     int  `json:"maxParallel"`
	Background      bool `json:"background"`
	AutoPopup       bool `json:"autoPopup"`
	RetryCount      int  `json:"retryCount"`
	RetryDelay      int  `json:"retryDelay"` // seconds
	ContinueOnError bool `json:"continueOnError"`
	// Big SFTP files (at least SegmentSize bytes, 0 = off) are copied in
	// Segments parts at the same time.
	SegmentSize int64 `json:"segmentSize"`
	Segments    int   `json:"segments"`
}

type EditorPrefs struct {
	Default      string        `json:"default"` // internal, external
	ExternalCmd  string        `json:"externalCmd"`
	TabSize      int           `json:"tabSize"`
	WordWrap     bool          `json:"wordWrap"`
	FontSize     int           `json:"fontSize"`
	MaxSizeMB    int           `json:"maxSizeMB"`
	Encoding     string        `json:"encoding"`
	AutoReload   bool          `json:"autoReload"`
	Associations []EditorAssoc `json:"associations"`
	// NoHistory stops keeping the previous versions of saved files.
	NoHistory bool `json:"noHistory,omitempty"`
	// ProjectExcludes are the folders a project skips (file list, search),
	// separated by spaces or commas; empty for the default list.
	ProjectExcludes string `json:"projectExcludes,omitempty"`
}

type EditorAssoc struct {
	Mask    string `json:"mask"`
	Command string `json:"command"` // "internal" or a command line with !.! for the file
}

type TerminalPrefs struct {
	FontSize     int    `json:"fontSize"`
	FontFamily   string `json:"fontFamily"`
	Scrollback   int    `json:"scrollback"`
	CursorBlink  bool   `json:"cursorBlink"`
	CopyOnSelect bool   `json:"copyOnSelect"`
	Term         string `json:"term"`
	// Snippets are saved commands typed into a terminal from its toolbar.
	Snippets []Snippet `json:"snippets"`
	// LocalShell is the shell of the local terminals ("" = the default one).
	LocalShell string `json:"localShell,omitempty"`
	// CopyKeys: "" or "ctrl" (Ctrl+C copies a selection, Ctrl+V pastes) or
	// "ctrlShift" (only Ctrl+Shift+C / Ctrl+Shift+V).
	CopyKeys string `json:"copyKeys,omitempty"`
	// RightClick: "" or "copyPaste" (copies the selection, else pastes, like
	// the Windows console) or "menu".
	RightClick string `json:"rightClick,omitempty"`
}

// Snippet is a saved terminal command; Run also presses Enter.
type Snippet struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	Run     bool   `json:"run"`
}

func defaultSnippets() []Snippet {
	return []Snippet{
		{ID: "df", Name: "Disk space", Command: "df -h", Run: true},
		{ID: "free", Name: "Memory", Command: "free -h", Run: true},
		{ID: "biggest", Name: "Biggest files here", Command: "du -ah . 2>/dev/null | sort -rh | head -20", Run: true},
		{ID: "logs", Name: "Last system logs", Command: "journalctl -n 100 --no-pager", Run: true},
	}
}

type LoggingPrefs struct {
	ToFile   bool   `json:"toFile"`
	Level    string `json:"level"` // normal, debug
	KeepDays int    `json:"keepDays"`
}

type Bookmark struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Side string `json:"side"` // local, remote
}

type TabState struct {
	SiteID string `json:"siteId,omitempty"`
	URL    string `json:"url,omitempty"`
	Remote string `json:"remote,omitempty"`
	Local  string `json:"local,omitempty"`
	// Project is a project tab, on the same connection (or this computer).
	Project *ProjectState `json:"project,omitempty"`
}

// ProjectState is a project tab to reopen.
type ProjectState struct {
	Root   string   `json:"root"`
	Files  []string `json:"files,omitempty"`
	Active string   `json:"active,omitempty"`
	Local  bool     `json:"local,omitempty"`
}

type Workspace struct {
	Name string     `json:"name"`
	Tabs []TabState `json:"tabs"`
}

type CustomCommand struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Command    string `json:"command"`
	Local      bool   `json:"local"`     // run on this computer instead of the server
	PerFile    bool   `json:"perFile"`   // run once per selected file
	ApplyDirs  bool   `json:"applyDirs"` // allowed on directories
	Recursive  bool   `json:"recursive"`
	ShowOutput bool   `json:"showOutput"`
	Shortcut   string `json:"shortcut,omitempty"`
}

// SSHPrefs controls the use of the OpenSSH client configuration.
type SSHPrefs struct {
	IgnoreConfig bool   `json:"ignoreConfig"`         // do not read ssh_config at all
	ConfigFile   string `json:"configFile,omitempty"` // replaces ~/.ssh/config
}

type WebPrefs struct {
	User         string `json:"user,omitempty"`
	PasswordHash string `json:"passwordHash,omitempty"`
}

type Prefs struct {
	Language  string `json:"language"`
	Theme     string `json:"theme"`
	Accent    string `json:"accent"`
	Density   string `json:"density"`
	FontSize  int    `json:"fontSize"`
	Interface string `json:"interface"` // commander, explorer

	ShowHidden    bool   `json:"showHidden"`
	DirsFirst     bool   `json:"dirsFirst"`
	SizeFormat    string `json:"sizeFormat"` // iec, si, bytes
	DateFormat    string `json:"dateFormat"` // relative, short, iso
	ShowFKeyBar   bool   `json:"showFKeyBar"`
	ShowTree      bool   `json:"showTree"`
	DoubleClick   string `json:"doubleClick"` // open, edit, copy
	ShowPreview   bool   `json:"showPreview"`
	FullRowSelect bool   `json:"fullRowSelect"`
	NaturalSort   bool   `json:"naturalSort"`

	ConfirmDelete    bool `json:"confirmDelete"`
	ConfirmTransfer  bool `json:"confirmTransfer"`
	ConfirmExit      bool `json:"confirmExit"`
	ConfirmDragDrop  bool `json:"confirmDragDrop"`
	ConfirmOverwrite bool `json:"confirmOverwrite"`

	Transfer TransferPrefs `json:"transfer"`
	Queue    QueuePrefs    `json:"queue"`
	Editor   EditorPrefs   `json:"editor"`
	Terminal TerminalPrefs `json:"terminal"`
	Logging  LoggingPrefs  `json:"logging"`

	RestoreTabs    bool `json:"restoreTabs"`
	AutoReconnect  bool `json:"autoReconnect"`
	ReconnectDelay int  `json:"reconnectDelay"`

	Bookmarks      map[string][]Bookmark `json:"bookmarks"`
	Workspaces     []Workspace           `json:"workspaces"`
	LastTabs       []TabState            `json:"lastTabs"`
	CustomCommands []CustomCommand       `json:"customCommands"`
	SSH            SSHPrefs              `json:"ssh"`
	Web            WebPrefs              `json:"web"`
	UI             map[string]any        `json:"ui"`
}

const DefaultTextMask = "*.*html; *.htm; *.txt; *.php; *.php3; *.cgi; *.c; *.cpp; *.h; *.pas; *.bas; *.tex; *.pl; *.js; .htaccess; *.xtml; *.css; *.cfg; *.ini; *.sh; *.xml; *.md; *.json; *.yml; *.yaml; *.csv; *.sql; *.py; *.rb; *.conf"

func DefaultPrefs() Prefs {
	return Prefs{
		Language: "auto", Theme: "system", Accent: "#4f7cff", Density: "comfortable", FontSize: 13, Interface: "commander",
		DirsFirst: true, SizeFormat: "iec", DateFormat: "short", ShowFKeyBar: true, DoubleClick: "open", NaturalSort: true,
		ConfirmDelete: true, ConfirmTransfer: false, ConfirmExit: true, ConfirmDragDrop: false, ConfirmOverwrite: true,
		Transfer: TransferPrefs{
			Mode: "binary", TextMask: DefaultTextMask, PreserveTime: true, PreservePerms: true, Perms: 0o644,
			AddXToDirs: true, Overwrite: "ask", ResumeThreshold: 100 * 1024, CaseMode: "none", ReplaceInvalid: true,
		},
		Queue:       QueuePrefs{MaxParallel: 3, Background: true, AutoPopup: true, RetryCount: 2, RetryDelay: 3, SegmentSize: 100 << 20, Segments: 4},
		Editor:      EditorPrefs{Default: "internal", TabSize: 4, WordWrap: false, FontSize: 13, MaxSizeMB: 20, Encoding: "auto", AutoReload: true},
		Terminal:    TerminalPrefs{FontSize: 13, FontFamily: "", Scrollback: 10000, CursorBlink: true, Term: "xterm-256color", Snippets: defaultSnippets()},
		Logging:     LoggingPrefs{ToFile: false, Level: "normal", KeepDays: 14},
		RestoreTabs: true, AutoReconnect: true, ReconnectDelay: 5,
		Bookmarks: map[string][]Bookmark{},
		CustomCommands: []CustomCommand{
			{ID: "tar", Name: "Archive (tar.gz)", Command: `tar -czvf "!?Archive name:?archive.tar.gz!" !&`, ApplyDirs: true, ShowOutput: true},
			{ID: "untar", Name: "Extract tar archive", Command: `tar -xvf "!"`, PerFile: true, ShowOutput: true},
			{ID: "unzip", Name: "Extract zip archive", Command: `unzip -o "!"`, PerFile: true, ShowOutput: true},
			{ID: "grep", Name: "Search text (grep)", Command: `grep -rn "!?Pattern:?!" !&`, ApplyDirs: true, ShowOutput: true},
			{ID: "du", Name: "Disk usage", Command: `du -sh !&`, ApplyDirs: true, ShowOutput: true},
			{ID: "exec", Name: "Execute", Command: `"./!"`, PerFile: true, ShowOutput: true},
		},
		UI: map[string]any{},
	}
}

func (p *Prefs) normalize() {
	d := DefaultPrefs()
	if p.Language == "" {
		p.Language = d.Language
	}
	if p.Theme == "" {
		p.Theme = d.Theme
	}
	if p.Accent == "" {
		p.Accent = d.Accent
	}
	if p.Density == "" {
		p.Density = d.Density
	}
	if p.FontSize == 0 {
		p.FontSize = d.FontSize
	}
	if p.Interface == "" {
		p.Interface = d.Interface
	}
	if p.SizeFormat == "" {
		p.SizeFormat = d.SizeFormat
	}
	if p.DateFormat == "" {
		p.DateFormat = d.DateFormat
	}
	if p.DoubleClick == "" {
		p.DoubleClick = d.DoubleClick
	}
	if p.Transfer.Mode == "" {
		p.Transfer = d.Transfer
	}
	if p.Transfer.Overwrite == "" {
		p.Transfer.Overwrite = "ask"
	}
	if p.Queue.MaxParallel <= 0 {
		p.Queue.MaxParallel = d.Queue.MaxParallel
	}
	if p.Queue.Segments == 0 { // settings written before segmented transfers
		p.Queue.Segments, p.Queue.SegmentSize = d.Queue.Segments, d.Queue.SegmentSize
	}
	if p.Editor.Default == "" {
		p.Editor = d.Editor
	}
	if p.Editor.MaxSizeMB <= 0 {
		p.Editor.MaxSizeMB = 20
	}
	if p.Terminal.FontSize == 0 {
		p.Terminal = d.Terminal
	}
	if p.Terminal.Snippets == nil { // settings written before snippets
		p.Terminal.Snippets = defaultSnippets()
	}
	if p.Logging.Level == "" {
		p.Logging = d.Logging
	}
	if p.Bookmarks == nil {
		p.Bookmarks = map[string][]Bookmark{}
	}
	if p.CustomCommands == nil {
		p.CustomCommands = d.CustomCommands
	}
	if p.UI == nil {
		p.UI = map[string]any{}
	}
}

func (p Prefs) clone() Prefs {
	data, _ := json.Marshal(p)
	var out Prefs
	json.Unmarshal(data, &out)
	out.normalize()
	return out
}
