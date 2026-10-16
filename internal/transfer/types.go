// Package transfer implements Skiff's transfer queue: background jobs that
// copy or move files between any two connections (local ↔ remote, remote ↔
// remote), with resume, overwrite policies, masks, text mode, speed limits,
// verification and parallel workers.
package transfer

import (
	"context"
	"time"

	"skiff/internal/vfs"
)

// Options mirrors WinSCP's transfer settings.
type Options struct {
	Mode             string `json:"mode"` // binary, text, auto
	TextMask         string `json:"textMask,omitempty"`
	PreserveTime     bool   `json:"preserveTime"`
	PreservePerms    bool   `json:"preservePerms"`
	SetPerms         bool   `json:"setPerms"`
	Perms            uint32 `json:"perms,omitempty"`
	AddXToDirs       bool   `json:"addXToDirs"`
	Overwrite        string `json:"overwrite"` // ask, overwrite, skip, newer, resume, rename, differentSize
	ResumeThreshold  int64  `json:"resumeThreshold"`
	FileMask         string `json:"fileMask,omitempty"`
	SpeedLimit       int64  `json:"speedLimit,omitempty"` // KiB/s
	CaseMode         string `json:"caseMode,omitempty"`
	Verify           bool   `json:"verify,omitempty"`
	ExcludeHidden    bool   `json:"excludeHidden,omitempty"`
	ReplaceInvalid   bool   `json:"replaceInvalid,omitempty"`
	IgnorePermErrors bool   `json:"ignorePermErrors,omitempty"`
	Move             bool   `json:"move,omitempty"`
	NewerOnly        bool   `json:"newerOnly,omitempty"`
	Parallel         int    `json:"parallel,omitempty"`
	RetryCount       int    `json:"retryCount,omitempty"`
	RetryDelay       int    `json:"retryDelay,omitempty"`
	ContinueOnError  bool   `json:"continueOnError,omitempty"`
	// SegmentSize > 0 splits files of at least that many bytes into
	// Segments parts copied in parallel (SFTP, each part on its own channel).
	SegmentSize int64 `json:"segmentSize,omitempty"`
	Segments    int   `json:"segments,omitempty"`
	// EOL of each side for text mode ("LF"/"CRLF"), filled in by the core.
	SrcEOL string `json:"srcEol,omitempty"`
	DstEOL string `json:"dstEol,omitempty"`
}

// Request describes what to transfer.
type Request struct {
	Src    string   `json:"src"`   // source connection id ("local")
	Dst    string   `json:"dst"`   // destination connection id
	Paths  []string `json:"paths"` // source files or directories
	DstDir string   `json:"dstDir"`
	// Names optionally renames items: source path -> target name.
	Names map[string]string `json:"names,omitempty"`
	// Files restricts a directory transfer to these relative paths (sync).
	Options Options `json:"options"`
	Label   string  `json:"label,omitempty"`
	// Explicit, when set, is a precomputed list of file pairs (used by sync).
	Explicit []Pair `json:"explicit,omitempty"`
}

// Pair is a single explicit file copy.
type Pair struct {
	Src   string    `json:"src"`
	Dst   string    `json:"dst"`
	IsDir bool      `json:"isDir,omitempty"`
	Size  int64     `json:"size,omitempty"`
	MTime time.Time `json:"mtime,omitempty"`
	Mode  uint32    `json:"mode,omitempty"`
	// Delete marks a deletion of Src (sync with "delete files").
	Delete bool `json:"delete,omitempty"`
}

type Status string

const (
	Queued    Status = "queued"
	Scanning  Status = "scanning"
	Running   Status = "running"
	Paused    Status = "paused"
	Done      Status = "done"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
	Partial   Status = "partial" // finished with errors/skips
)

type FileError struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

type Active struct {
	Name  string  `json:"name"`
	Size  int64   `json:"size"`
	Done  int64   `json:"done"`
	Speed float64 `json:"speed"`
	Parts int     `json:"parts,omitempty"` // segments copied in parallel
}

// Snapshot is the public state of a job.
type Snapshot struct {
	ID           string    `json:"id"`
	Label        string    `json:"label"`
	Src          string    `json:"src"`
	Dst          string    `json:"dst"`
	SrcTitle     string    `json:"srcTitle"`
	DstTitle     string    `json:"dstTitle"`
	DstDir       string    `json:"dstDir"`
	Move         bool      `json:"move"`
	Status       Status    `json:"status"`
	Created      time.Time `json:"created"`
	Started      time.Time `json:"started,omitempty"`
	Finished     time.Time `json:"finished,omitempty"`
	TotalBytes   int64     `json:"totalBytes"`
	DoneBytes    int64     `json:"doneBytes"`
	TotalFiles   int       `json:"totalFiles"`
	DoneFiles    int       `json:"doneFiles"`
	FailedFiles  int       `json:"failedFiles"`
	SkippedFiles int       `json:"skippedFiles"`
	// SegmentedFiles counts the files copied in parallel parts.
	SegmentedFiles int         `json:"segmentedFiles,omitempty"`
	Speed          float64     `json:"speed"`
	ETA            float64     `json:"eta"`
	Active         []Active    `json:"active"`
	Error          string      `json:"error,omitempty"`
	Errors         []FileError `json:"errors,omitempty"`
	Paths          []string    `json:"paths"`
}

// Resolver gives the engine access to connections.
type Resolver interface {
	// Acquire returns an FS for background use and a release function.
	Acquire(ctx context.Context, conn string) (vfs.FS, func(), error)
	Title(conn string) string
	// Changed notifies that a directory content changed (UI refresh).
	Changed(conn, dir string)
}

// Publisher emits events to the UI.
type Publisher interface {
	Publish(typ string, data any)
}
