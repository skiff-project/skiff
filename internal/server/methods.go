package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"skiff/internal/config"
	"skiff/internal/core"
	"skiff/internal/devsync"
	"skiff/internal/importer"
	"skiff/internal/keys"
	"skiff/internal/prompt"
	"skiff/internal/sshconfig"
	"skiff/internal/sshx"
	"skiff/internal/tasks"
	"skiff/internal/transfer"
	"skiff/internal/vfs"
)

type empty struct{}

type ok struct {
	OK bool `json:"ok"`
}

var yes = ok{OK: true}

func (s *Server) web() bool { return s.opts.Mode == Web }

func (s *Server) registerRPC() {
	c := s.core

	// ---- application ----
	method(s, "app.info", func(ctx context.Context, _ empty) (any, error) {
		home, _ := c.Local.Home(ctx)
		host, _ := os.Hostname()
		return map[string]any{
			"version":       s.opts.Version,
			"mode":          s.opts.Mode,
			"os":            runtime.GOOS,
			"arch":          runtime.GOARCH,
			"hostname":      host,
			"configDir":     c.Store.Dir,
			"portable":      c.Store.Portable,
			"localHome":     home,
			"localStyle":    c.Local.Caps().PathStyle,
			"localCaps":     c.Local.Caps(),
			"locked":        c.Store.Secrets.Locked(),
			"hasMaster":     c.Store.Secrets.HasMaster(),
			"localRoot":     c.Local.Root,
			"agent":         sshx.AgentAvailable(),
			"localTerminal": (!s.web() || s.opts.AllowLocalTerminal) && c.Local.Root == "",
			"startupUrl":    s.takeStartupURL(),
		}, nil
	})
	method(s, "prefs.get", func(ctx context.Context, _ empty) (config.Prefs, error) {
		p := c.Store.Prefs()
		p.Web = config.WebPrefs{}
		return p, nil
	})
	method(s, "prefs.set", func(ctx context.Context, p config.Prefs) (ok, error) {
		p.Web = c.Store.Prefs().Web
		if err := c.Store.SetPrefs(p); err != nil {
			return ok{}, err
		}
		c.Transfers.SetMaxParallel(p.Queue.MaxParallel)
		c.Bus.Publish("prefs.changed", nil)
		s.changed()
		return yes, nil
	})

	// ---- sites ----
	type sitesList struct {
		Folders []string          `json:"folders"`
		Sites   []config.SiteView `json:"sites"`
	}
	method(s, "sites.list", func(ctx context.Context, _ empty) (sitesList, error) {
		out := sitesList{Folders: c.Store.Folders(), Sites: []config.SiteView{}}
		for _, st := range c.Store.Sites() {
			out.Sites = append(out.Sites, config.View(st))
		}
		return out, nil
	})
	type saveSite struct {
		Site    config.Site        `json:"site"`
		Secrets config.SecretInput `json:"secrets"`
	}
	method(s, "sites.save", func(ctx context.Context, p saveSite) (config.SiteView, error) {
		st, err := c.Store.SaveSite(p.Site, p.Secrets)
		return config.View(st), err
	})
	type ids struct {
		IDs []string `json:"ids"`
	}
	method(s, "sites.delete", func(ctx context.Context, p ids) (ok, error) {
		return yes, c.Store.DeleteSites(p.IDs)
	})
	type idp struct {
		ID string `json:"id"`
	}
	type dupReq struct {
		ID   string `json:"id"`
		Name string `json:"name,omitempty"` // name of the copy (localised by the UI)
	}
	method(s, "sites.duplicate", func(ctx context.Context, p dupReq) (config.SiteView, error) {
		st, err := c.Store.DuplicateSite(p.ID, p.Name)
		return config.View(st), err
	})
	type moveSites struct {
		IDs    []string `json:"ids"`
		Folder string   `json:"folder"`
	}
	method(s, "sites.move", func(ctx context.Context, p moveSites) (ok, error) {
		return yes, c.Store.MoveSites(p.IDs, p.Folder)
	})
	type folderOp struct {
		Name      string `json:"name"`
		To        string `json:"to"`
		WithSites bool   `json:"withSites"`
	}
	method(s, "folders.add", func(ctx context.Context, p folderOp) (ok, error) { return yes, c.Store.AddFolder(p.Name) })
	method(s, "folders.rename", func(ctx context.Context, p folderOp) (ok, error) {
		return yes, c.Store.RenameFolder(p.Name, p.To)
	})
	method(s, "folders.delete", func(ctx context.Context, p folderOp) (ok, error) {
		return yes, c.Store.DeleteFolder(p.Name, p.WithSites)
	})
	type urlReq struct {
		ID           string `json:"id"`
		WithPassword bool   `json:"withPassword"`
	}
	method(s, "sites.url", func(ctx context.Context, p urlReq) (string, error) {
		st, found := c.Store.Site(p.ID)
		if !found {
			return "", errors.New("site not found")
		}
		pw := ""
		if p.WithPassword {
			r, err := c.Store.Resolve(st)
			if err != nil {
				return "", err
			}
			pw = r.Password
		}
		return st.URL(p.WithPassword, pw), nil
	})
	method(s, "sites.parseUrl", func(ctx context.Context, p struct {
		URL string `json:"url"`
	}) (map[string]any, error) {
		st, pw, err := config.ParseURL(p.URL)
		if err != nil {
			return nil, err
		}
		return map[string]any{"site": st, "password": pw}, nil
	})
	method(s, "sites.importSources", func(ctx context.Context, _ empty) ([]importer.Source, error) {
		return importer.Sources(), nil
	})
	type importReq struct {
		Source string `json:"source"`
		Path   string `json:"path,omitempty"`
		Data   string `json:"data,omitempty"`
	}
	method(s, "sites.importPreview", func(ctx context.Context, p importReq) ([]importer.Item, error) {
		if s.web() && (p.Path != "" || (p.Data == "" && p.Source != "ssh-config")) {
			return nil, errors.New("in web mode, paste or upload the file content")
		}
		return importer.Load(p.Source, p.Path, []byte(p.Data))
	})
	type importCommit struct {
		Items  []importer.Item `json:"items"`
		Folder string          `json:"folder"`
	}
	method(s, "sites.importCommit", func(ctx context.Context, p importCommit) (int, error) {
		n := 0
		for _, it := range p.Items {
			st := it.Site
			if p.Folder != "" {
				if st.Folder != "" {
					st.Folder = p.Folder + "/" + st.Folder
				} else {
					st.Folder = p.Folder
				}
			}
			if _, err := c.Store.ImportSite(st, it.Password, it.Passphrase); err != nil {
				return n, err
			}
			n++
		}
		return n, nil
	})
	method(s, "sites.export", func(ctx context.Context, p struct {
		IDs    []string `json:"ids"`
		Format string   `json:"format"`
	}) (string, error) {
		var list []config.Site
		for _, st := range c.Store.Sites() {
			for _, id := range p.IDs {
				if st.ID == id {
					list = append(list, st)
				}
			}
		}
		if len(p.IDs) == 0 {
			list = c.Store.Sites()
		}
		return importer.Export(list, p.Format)
	})

	// ---- security ----
	method(s, "security.unlock", func(ctx context.Context, p struct {
		Password string `json:"password"`
	}) (ok, error) {
		if err := c.Store.Secrets.Unlock(p.Password); err != nil {
			return ok{}, err
		}
		c.Bus.Publish("security.changed", nil)
		return yes, nil
	})
	method(s, "security.lock", func(ctx context.Context, _ empty) (ok, error) {
		c.Store.Secrets.Lock()
		c.Bus.Publish("security.changed", nil)
		return yes, nil
	})
	method(s, "security.setMaster", func(ctx context.Context, p struct {
		Old string `json:"old"`
		New string `json:"new"`
	}) (ok, error) {
		if err := c.Store.SetMasterPassword(p.Old, p.New); err != nil {
			return ok{}, err
		}
		c.Bus.Publish("security.changed", nil)
		return yes, nil
	})
	method(s, "hostkeys.list", func(ctx context.Context, _ empty) ([]sshx.KnownHost, error) {
		l, err := c.HostKeys.List()
		if l == nil {
			l = []sshx.KnownHost{}
		}
		return l, err
	})
	method(s, "hostkeys.remove", func(ctx context.Context, p struct {
		Line int `json:"line"`
	}) (ok, error) {
		return yes, c.HostKeys.Remove(p.Line)
	})

	// ---- scheduled tasks ----
	method(s, "tasks.list", func(ctx context.Context, _ empty) ([]tasks.View, error) {
		return s.tasks.Views(), nil
	})
	method(s, "tasks.save", func(ctx context.Context, t tasks.Task) (tasks.View, error) {
		saved, err := s.tasks.Store.Save(t)
		if err != nil {
			return tasks.View{}, err
		}
		s.tasks.Changed(saved.ID)
		return s.tasks.View(saved), nil
	})
	method(s, "tasks.delete", func(ctx context.Context, p idp) (ok, error) {
		s.tasks.Cancel(p.ID)
		if err := s.tasks.Store.Delete(p.ID); err != nil {
			return ok{}, err
		}
		s.tasks.Changed(p.ID)
		return yes, nil
	})
	method(s, "tasks.run", func(ctx context.Context, p idp) (ok, error) {
		return yes, s.tasks.RunNow(p.ID, "manual")
	})
	method(s, "tasks.cancel", func(ctx context.Context, p idp) (ok, error) {
		s.tasks.Cancel(p.ID)
		return yes, nil
	})
	method(s, "tasks.runs", func(ctx context.Context, p struct {
		ID string `json:"id,omitempty"`
	}) ([]tasks.Run, error) {
		return s.tasks.Store.Runs(p.ID), nil
	})

	// ---- containers ----
	method(s, "containers.list", func(ctx context.Context, p struct {
		Runtime   string `json:"runtime"`
		Command   string `json:"command,omitempty"`
		Via       string `json:"via,omitempty"` // an open SSH connection, "" = this computer
		Context   string `json:"context,omitempty"`
		Namespace string `json:"namespace,omitempty"`
	}) ([]core.ContainerInfo, error) {
		if p.Via == "" && !s.localExecAllowed() {
			return nil, errLocalContainers
		}
		return c.ListContainers(ctx, p.Runtime, p.Command, p.Via, p.Context, p.Namespace)
	})
	method(s, "containers.runtimes", func(ctx context.Context, p struct {
		Via string `json:"via,omitempty"`
	}) ([]string, error) {
		if p.Via == "" && !s.localExecAllowed() {
			return nil, errLocalContainers
		}
		return c.ContainerRuntimes(ctx, p.Via)
	})
	method(s, "containers.action", func(ctx context.Context, p struct {
		Runtime string `json:"runtime"`
		Command string `json:"command,omitempty"`
		Via     string `json:"via,omitempty"`
		Name    string `json:"name"`
		Action  string `json:"action"` // start, stop, restart
	}) (ok, error) {
		if p.Via == "" && !s.localExecAllowed() {
			return ok{}, errLocalContainers
		}
		return yes, c.ContainerAction(ctx, p.Runtime, p.Command, p.Via, p.Name, p.Action)
	})

	// ---- server monitor ----
	method(s, "monitor.sample", func(ctx context.Context, p struct {
		Conn string `json:"conn"`
		Key  string `json:"key"`
	}) (core.MonitorSample, error) {
		return c.MonitorSample(ctx, p.Conn, p.Key)
	})
	method(s, "monitor.kill", func(ctx context.Context, p struct {
		Conn   string `json:"conn"`
		PID    int    `json:"pid"`
		Signal string `json:"signal"`
	}) (ok, error) {
		return yes, c.MonitorKill(ctx, p.Conn, p.PID, p.Signal)
	})

	// ---- OpenSSH configuration ----
	type sshHost struct {
		sshconfig.HostEntry
		HostName string `json:"hostName"`
		User     string `json:"user,omitempty"`
		Port     int    `json:"port"`
	}
	type sshConfigInfo struct {
		Enabled    bool             `json:"enabled"`
		UserFile   string           `json:"userFile"`
		SystemFile string           `json:"systemFile"`
		Files      []sshconfig.File `json:"files"`
		Hosts      []sshHost        `json:"hosts"`
	}
	method(s, "sshconfig.info", func(ctx context.Context, _ empty) (sshConfigInfo, error) {
		cfg := c.SSHConfig()
		out := sshConfigInfo{Enabled: cfg != nil, Hosts: []sshHost{}}
		if cfg == nil {
			// still tell where the files would be read from
			cfg = sshconfig.Default()
			if f := c.Store.Prefs().SSH.ConfigFile; f != "" {
				cfg.UserFile = f
			}
		} else {
			for _, e := range cfg.Hosts() {
				h := cfg.Resolve(e.Alias, sshconfig.Query{})
				sh := sshHost{HostEntry: e, HostName: h.HostName(), Port: h.Port()}
				if h.Has("User") {
					sh.User = h.User()
				}
				out.Hosts = append(out.Hosts, sh)
			}
		}
		out.UserFile, out.SystemFile, out.Files = cfg.UserFile, cfg.SystemFile, cfg.Files()
		return out, nil
	})
	method(s, "sshconfig.resolve", func(ctx context.Context, p struct {
		Site config.Site `json:"site"`
	}) (core.SSHConfigReport, error) {
		return c.SSHConfigReport(p.Site), nil
	})

	// ---- connections ----
	type openReq struct {
		SiteID   string       `json:"siteId,omitempty"`
		Site     *config.Site `json:"site,omitempty"`
		Password string       `json:"password,omitempty"`
		URL      string       `json:"url,omitempty"`
		Token    string       `json:"token,omitempty"` // lets the UI abort the attempt
	}
	method(s, "conn.open", func(ctx context.Context, p openReq) (core.ConnInfo, error) {
		if p.URL != "" {
			st, pw, err := config.ParseURL(p.URL)
			if err != nil {
				return core.ConnInfo{}, err
			}
			p.Site = &st
			if p.Password == "" {
				p.Password = pw
			}
		}
		if site := p.Site; site != nil || p.SiteID != "" {
			if site == nil {
				st, _ := c.Store.Site(p.SiteID)
				site = &st
			}
			if site.Protocol == config.ProtoContainer && site.ViaConn == "" && site.ViaSite == "" && !s.localExecAllowed() {
				return core.ConnInfo{}, errLocalContainers
			}
		}
		// The connection must survive the HTTP request.
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if p.Token != "" {
			s.pending.Store(p.Token, cancel)
			defer s.pending.Delete(p.Token)
		}
		conn, err := c.Connect(cctx, p.SiteID, p.Site, p.Password)
		if err != nil {
			return core.ConnInfo{}, err
		}
		return conn.Info(), nil
	})
	type connReq struct {
		Conn string `json:"conn"`
	}
	method(s, "conn.abort", func(ctx context.Context, p struct {
		Token string `json:"token"`
	}) (ok, error) {
		if v, found := s.pending.Load(p.Token); found {
			v.(context.CancelFunc)()
		}
		return yes, nil
	})
	method(s, "conn.close", func(ctx context.Context, p connReq) (ok, error) { return yes, c.Disconnect(p.Conn) })
	method(s, "conn.reconnect", func(ctx context.Context, p connReq) (core.ConnInfo, error) {
		cn, err := c.Conn(p.Conn)
		if err != nil {
			return core.ConnInfo{}, err
		}
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := cn.Reconnect(cctx); err != nil {
			return core.ConnInfo{}, err
		}
		return cn.Info(), nil
	})
	method(s, "conn.list", func(ctx context.Context, _ empty) ([]core.ConnInfo, error) { return c.Conns(), nil })
	method(s, "conn.serverInfo", func(ctx context.Context, p connReq) ([]vfs.InfoItem, error) { return c.ServerInfo(p.Conn) })
	method(s, "conn.log", func(ctx context.Context, p connReq) ([]core.LogLine, error) {
		cn, err := c.Conn(p.Conn)
		if err != nil {
			return nil, err
		}
		return cn.Log().Lines(), nil
	})

	// ---- file system ----
	type pathReq struct {
		Conn string `json:"conn"`
		Path string `json:"path"`
	}
	method(s, "fs.list", func(ctx context.Context, p pathReq) (core.ListResult, error) { return c.List(ctx, p.Conn, p.Path) })
	method(s, "fs.stat", func(ctx context.Context, p pathReq) (vfs.Entry, error) { return c.Stat(ctx, p.Conn, p.Path) })
	method(s, "fs.home", func(ctx context.Context, p pathReq) (string, error) {
		f, err := c.FSFor(ctx, p.Conn)
		if err != nil {
			return "", err
		}
		return f.Home(ctx)
	})
	method(s, "fs.mkdir", func(ctx context.Context, p struct {
		Conn string `json:"conn"`
		Path string `json:"path"`
		Mode uint32 `json:"mode"`
	}) (ok, error) {
		return yes, c.Mkdir(ctx, p.Conn, p.Path, p.Mode)
	})
	method(s, "fs.createFile", func(ctx context.Context, p struct {
		Conn    string `json:"conn"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}) (ok, error) {
		return yes, c.CreateFile(ctx, p.Conn, p.Path, []byte(p.Content))
	})
	method(s, "fs.rename", func(ctx context.Context, p struct {
		Conn string `json:"conn"`
		From string `json:"from"`
		To   string `json:"to"`
	}) (ok, error) {
		return yes, c.Rename(ctx, p.Conn, p.From, p.To)
	})
	type multi struct {
		Conn    string            `json:"conn"`
		Paths   []string          `json:"paths"`
		DstDir  string            `json:"dstDir"`
		Names   map[string]string `json:"names,omitempty"`
		Recycle bool              `json:"recycle"`
	}
	method(s, "fs.move", func(ctx context.Context, p multi) (ok, error) {
		return yes, c.MoveTo(ctx, p.Conn, p.Paths, p.DstDir)
	})
	method(s, "fs.duplicate", func(ctx context.Context, p multi) (string, error) {
		return c.Duplicate(ctx, p.Conn, p.Paths, p.DstDir, p.Names)
	})
	method(s, "fs.delete", func(ctx context.Context, p multi) (ok, error) {
		return yes, c.Delete(ctx, p.Conn, p.Paths, p.Recycle)
	})
	method(s, "fs.setAttrs", func(ctx context.Context, p struct {
		Conn  string     `json:"conn"`
		Paths []string   `json:"paths"`
		Attrs core.Attrs `json:"attrs"`
	}) (ok, error) {
		return yes, c.SetAttrs(ctx, p.Conn, p.Paths, p.Attrs)
	})
	method(s, "fs.link", func(ctx context.Context, p struct {
		Conn   string `json:"conn"`
		Target string `json:"target"`
		Link   string `json:"link"`
		Hard   bool   `json:"hard"`
	}) (ok, error) {
		return yes, c.Link(ctx, p.Conn, p.Target, p.Link, p.Hard)
	})
	method(s, "fs.size", func(ctx context.Context, p multi) (core.SizeResult, error) { return c.CalcSize(ctx, p.Conn, p.Paths) })
	method(s, "fs.checksum", func(ctx context.Context, p struct {
		Conn string `json:"conn"`
		Path string `json:"path"`
		Algo string `json:"algo"`
	}) (string, error) {
		return c.Checksum(ctx, p.Conn, p.Path, p.Algo)
	})
	method(s, "fs.space", func(ctx context.Context, p pathReq) (core.SpaceResult, error) { return c.Space(ctx, p.Conn, p.Path) })
	method(s, "fs.exec", func(ctx context.Context, p struct {
		Conn    string `json:"conn"`
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
	}) (core.ExecResult, error) {
		if p.Conn == core.LocalID && s.web() {
			return core.ExecResult{}, errors.New("local commands are disabled in web mode")
		}
		return c.Exec(ctx, p.Conn, p.Command, p.Cwd)
	})
	method(s, "fs.owners", func(ctx context.Context, p connReq) (map[string][]string, error) {
		f, err := c.FSFor(ctx, p.Conn)
		if err != nil {
			return nil, err
		}
		if o, ok := f.(interface{ Owners() ([]string, []string) }); ok {
			u, g := o.Owners()
			return map[string][]string{"users": u, "groups": g}, nil
		}
		return map[string][]string{"users": {}, "groups": {}}, nil
	})
	method(s, "fs.find", func(ctx context.Context, p struct {
		Conn string           `json:"conn"`
		Dir  string           `json:"dir"`
		Opts core.FindOptions `json:"opts"`
	}) (string, error) {
		return c.Find(p.Conn, p.Dir, p.Opts)
	})
	method(s, "fs.findCancel", func(ctx context.Context, p idp) (ok, error) {
		c.CancelFind(p.ID)
		return yes, nil
	})
	method(s, "fs.watchLocal", func(ctx context.Context, p struct {
		Dirs []string `json:"dirs"`
	}) (ok, error) {
		return yes, c.WatchLocalDirs(p.Dirs)
	})
	method(s, "fs.readText", func(ctx context.Context, p struct {
		Conn     string `json:"conn"`
		Path     string `json:"path"`
		Encoding string `json:"encoding"`
	}) (core.FileContent, error) {
		return c.ReadText(ctx, p.Conn, p.Path, p.Encoding)
	})
	method(s, "fs.writeText", func(ctx context.Context, p core.WriteText) (vfs.Entry, error) { return c.WriteText(ctx, p) })
	method(s, "fs.open", func(ctx context.Context, p pathReq) (ok, error) {
		if s.web() {
			return ok{}, errors.New("not available in web mode")
		}
		return yes, c.OpenRemote(ctx, p.Conn, p.Path)
	})
	method(s, "fs.reveal", func(ctx context.Context, p pathReq) (ok, error) {
		if s.web() {
			return ok{}, errors.New("not available in web mode")
		}
		return yes, core.RevealInFileManager(p.Path)
	})

	// ---- transfers ----
	method(s, "transfer.defaults", func(ctx context.Context, p struct {
		Src string `json:"src"`
		Dst string `json:"dst"`
	}) (transfer.Options, error) {
		return c.DefaultTransferOptions(p.Src, p.Dst), nil
	})
	method(s, "transfer.enqueue", func(ctx context.Context, p transfer.Request) (string, error) {
		if len(p.Paths) == 0 && len(p.Explicit) == 0 {
			return "", errors.New("nothing to transfer")
		}
		if p.Options.Mode == "" {
			o := c.DefaultTransferOptions(p.Src, p.Dst)
			o.Move = p.Options.Move
			p.Options = o
		}
		return c.Enqueue(p), nil
	})
	method(s, "transfer.list", func(ctx context.Context, _ empty) ([]transfer.Snapshot, error) { return c.Transfers.Jobs(), nil })
	type jobReq struct {
		ID    string `json:"id"`
		Value bool   `json:"value"`
		Delta int    `json:"delta"`
	}
	method(s, "transfer.cancel", func(ctx context.Context, p jobReq) (ok, error) { c.Transfers.Cancel(p.ID); return yes, nil })
	method(s, "transfer.pause", func(ctx context.Context, p jobReq) (ok, error) {
		c.Transfers.Pause(p.ID, p.Value)
		c.Transfers.Hold(p.ID, p.Value)
		return yes, nil
	})
	method(s, "transfer.move", func(ctx context.Context, p jobReq) (ok, error) { c.Transfers.Move(p.ID, p.Delta); return yes, nil })
	method(s, "transfer.remove", func(ctx context.Context, p jobReq) (ok, error) { c.Transfers.Remove(p.ID); return yes, nil })
	method(s, "transfer.clear", func(ctx context.Context, _ empty) (ok, error) { c.Transfers.ClearFinished(); return yes, nil })
	method(s, "transfer.retry", func(ctx context.Context, p jobReq) (string, error) { return c.Transfers.Retry(p.ID) })

	// ---- synchronization ----
	method(s, "sync.compare", func(ctx context.Context, p core.SyncRequest) (*core.SyncPlan, error) {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		go func() {
			<-ctx.Done()
			cancel()
		}()
		return c.SyncCompare(cctx, p)
	})
	method(s, "sync.execute", func(ctx context.Context, p struct {
		PlanID  string            `json:"planId"`
		IDs     []string          `json:"ids"`
		Options *transfer.Options `json:"options,omitempty"`
	}) (core.SyncResult, error) {
		return c.SyncExecute(context.Background(), p.PlanID, p.IDs, p.Options)
	})
	method(s, "watch.start", func(ctx context.Context, p core.WatchRequest) (core.WatchInfo, error) { return c.StartWatch(p) })
	method(s, "watch.stop", func(ctx context.Context, p idp) (ok, error) { c.StopWatch(p.ID); return yes, nil })
	method(s, "watch.list", func(ctx context.Context, _ empty) ([]core.WatchInfo, error) { return c.Watches(), nil })

	// ---- editors ----
	method(s, "edit.external", func(ctx context.Context, p struct {
		Conn   string `json:"conn"`
		Path   string `json:"path"`
		Editor string `json:"editor"`
	}) (any, error) {
		if s.web() {
			return nil, errors.New("external editors are not available in web mode")
		}
		ed, err := c.EditExternal(ctx, p.Conn, p.Path, p.Editor)
		if ed == nil {
			return nil, err
		}
		return *ed, err
	})
	method(s, "edit.list", func(ctx context.Context, _ empty) ([]core.ExternalEdit, error) { return c.ExternalEdits(), nil })
	method(s, "edit.stop", func(ctx context.Context, p idp) (ok, error) { c.StopExternalEdit(p.ID); return yes, nil })

	// ---- terminals ----
	method(s, "term.open", func(ctx context.Context, p struct {
		Conn string `json:"conn"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
		Cwd   string `json:"cwd"`
		Shell string `json:"shell,omitempty"` // ID from term.shells
	}) (core.Terminal, error) {
		if p.Conn == core.LocalID && s.web() && !s.opts.AllowLocalTerminal {
			return core.Terminal{}, errors.New("the local terminal is disabled in web mode")
		}
		t, err := c.OpenTerminal(p.Conn, p.Cols, p.Rows, p.Cwd, p.Shell)
		if err != nil {
			return core.Terminal{}, err
		}
		return core.Terminal{ID: t.ID, Conn: t.Conn, Title: t.Title, Created: t.Created}, nil
	})
	method(s, "term.close", func(ctx context.Context, p idp) (ok, error) { c.CloseTerminal(p.ID); return yes, nil })
	method(s, "term.shells", func(ctx context.Context, p connReq) ([]core.Shell, error) {
		if p.Conn == core.LocalID && !s.localExecAllowed() {
			return []core.Shell{}, nil
		}
		return c.Shells(ctx, p.Conn)
	})
	method(s, "term.list", func(ctx context.Context, _ empty) ([]core.Terminal, error) { return c.Terminals(), nil })

	// ---- tunnels ----
	method(s, "tunnel.start", func(ctx context.Context, p struct {
		Conn    string         `json:"conn"`
		Forward config.Forward `json:"forward"`
	}) (core.TunnelInfo, error) {
		return c.StartTunnel(p.Conn, p.Forward)
	})
	method(s, "tunnel.stop", func(ctx context.Context, p idp) (ok, error) { c.StopTunnel(p.ID); return yes, nil })
	method(s, "tunnel.list", func(ctx context.Context, _ empty) ([]core.TunnelInfo, error) { return c.Tunnels(), nil })

	// ---- prompts ----
	method(s, "prompt.answer", func(ctx context.Context, p struct {
		ID       string          `json:"id"`
		Response prompt.Response `json:"response"`
	}) (ok, error) {
		return yes, c.Broker.Answer(p.ID, p.Response)
	})
	method(s, "prompt.pending", func(ctx context.Context, _ empty) ([]core.PromptEvent, error) { return c.Broker.Pending(), nil })

	// ---- keys ----
	method(s, "keys.generate", func(ctx context.Context, p keys.GenerateRequest) (keys.Generated, error) {
		if s.web() && p.SaveTo != "" {
			if _, err := c.Local.Stat(ctx, vfs.Dir(c.Local.Caps().PathStyle, p.SaveTo)); err != nil {
				return keys.Generated{}, err
			}
		}
		g, err := keys.Generate(p)
		if err == nil && p.Keychain {
			name := p.Name
			if name == "" {
				name = p.Comment
			}
			_, err = addToKeychain(c, keys.ConvertRequest{Data: g.Private, Passphrase: p.Passphrase}, name)
		}
		return g, err
	})
	method(s, "keys.list", func(ctx context.Context, _ empty) ([]keys.KeyFile, error) { return keys.List(), nil })
	method(s, "keys.install", func(ctx context.Context, p struct {
		Conn      string `json:"conn"`
		PublicKey string `json:"publicKey"`
	}) (string, error) {
		return keys.Install(ctx, c, p.Conn, p.PublicKey)
	})
	// ---- Skiff keychain ----
	type keychainList struct {
		Keys   []config.KeychainKey `json:"keys"`
		Locked bool                 `json:"locked"`
	}
	method(s, "keychain.list", func(ctx context.Context, _ empty) (keychainList, error) {
		return keychainList{Keys: c.Store.Keys(), Locked: c.Store.Secrets.Locked()}, nil
	})
	type keychainAdd struct {
		Name       string `json:"name"`
		Path       string `json:"path,omitempty"` // a key file, or
		Data       string `json:"data,omitempty"` // its content
		Passphrase string `json:"passphrase,omitempty"`
	}
	method(s, "keychain.add", func(ctx context.Context, p keychainAdd) (config.KeychainKey, error) {
		if s.web() && p.Path != "" {
			return config.KeychainKey{}, errors.New("in web mode, paste the key instead of giving a path")
		}
		k, err := addToKeychain(c, keys.ConvertRequest{Path: p.Path, Data: p.Data, Passphrase: p.Passphrase}, p.Name)
		if err == nil {
			s.changed()
		}
		return k, err
	})
	method(s, "keychain.rename", func(ctx context.Context, p struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}) (ok, error) {
		s.changed()
		return yes, c.Store.RenameKey(p.ID, p.Name)
	})
	method(s, "keychain.remove", func(ctx context.Context, p idp) (ok, error) {
		s.changed()
		return yes, c.Store.RemoveKey(p.ID)
	})

	// ---- sync between devices ----
	method(s, "devsync.get", func(ctx context.Context, _ empty) (devsync.View, error) {
		return s.devsync.View(), nil
	})
	method(s, "devsync.set", func(ctx context.Context, p struct {
		Config     devsync.Config `json:"config"`
		Passphrase *string        `json:"passphrase,omitempty"`
	}) (devsync.View, error) {
		err := s.devsync.SetConfig(p.Config, p.Passphrase)
		return s.devsync.View(), err
	})
	method(s, "devsync.now", func(ctx context.Context, _ empty) (devsync.Result, error) {
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		return s.devsync.Sync(cctx)
	})

	method(s, "keys.convert", func(ctx context.Context, p keys.ConvertRequest) (keys.Generated, error) {
		if s.web() && (p.Path != "" || p.SaveTo != "") {
			return keys.Generated{}, errors.New("in web mode, paste the key instead of giving a path")
		}
		return keys.Convert(p)
	})
}

// addToKeychain stores a private key (any format, decrypted with its
// passphrase) in the keychain.
func addToKeychain(c *core.Core, r keys.ConvertRequest, name string) (config.KeychainKey, error) {
	r.Format, r.NewPassphrase, r.SaveTo = "openssh", "", ""
	g, err := keys.Convert(r)
	if err != nil {
		var pm *ssh.PassphraseMissingError
		if errors.As(err, &pm) {
			return config.KeychainKey{}, errors.New("this key is protected by a passphrase")
		}
		return config.KeychainKey{}, err
	}
	if name == "" {
		name = keyName(g, r.Path)
	}
	return c.Store.AddKey(config.KeychainKey{Name: name, Type: g.Type, Fingerprint: g.Fingerprint, PublicKey: strings.TrimSpace(g.Public)}, g.Private)
}

// keyName names a key added without a name: its comment (in the key, or in
// the .pub file next to it), else its type and the start of its fingerprint.
func keyName(g keys.Generated, path string) string {
	if f := strings.Fields(g.Public); len(f) > 2 {
		return strings.Join(f[2:], " ")
	}
	if path != "" {
		if b, err := os.ReadFile(path + ".pub"); err == nil {
			if f := strings.Fields(string(b)); len(f) > 2 {
				return strings.Join(f[2:], " ")
			}
		}
		return filepath.Base(path)
	}
	fp := strings.TrimPrefix(g.Fingerprint, "SHA256:")
	return g.Type + " " + fp[:min(8, len(fp))]
}

var errLocalContainers = errors.New("the containers of the server running Skiff are disabled in web mode (see --allow-local-terminal)")

// localExecAllowed tells whether commands may run on the computer running
// Skiff for the user (local terminal, local containers).
func (s *Server) localExecAllowed() bool {
	return (!s.web() || s.opts.AllowLocalTerminal) && s.core.Local.Root == ""
}
