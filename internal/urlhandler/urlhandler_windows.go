//go:build windows

package urlhandler

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The Windows way, for the user only (no administrator rights): each scheme
// is a URL protocol of HKCU\Software\Classes that starts Skiff, and Skiff
// declares itself in RegisteredApplications so that it shows in Settings >
// Default apps. A choice made there (UserChoice) wins over the protocol;
// Windows protects it, so only the user can change it.

const (
	progID   = "Skiff.URL"
	capsPath = `Software\Skiff\Capabilities`
	classes  = `Software\Classes\`
)

func command(exe string) string { return `"` + exe + `" --link "%1"` }

// isSkiff: a command that starts Skiff
func isSkiff(cmd string) bool {
	return strings.Contains(strings.ToLower(cmd), "skiff") && strings.Contains(cmd, "--link")
}

func readString(root registry.Key, path, name string) string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue(name)
	return v
}

// exeName is the program of a command line, for display.
func exeName(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		if i := strings.Index(cmd[1:], `"`); i >= 0 {
			cmd = cmd[1 : i+1]
		}
	} else if f := strings.Fields(cmd); len(f) > 0 {
		cmd = f[0]
	}
	return strings.TrimSuffix(filepath.Base(cmd), filepath.Ext(cmd))
}

func get() Status {
	st := Status{Supported: true, CanRemove: true, Settings: true}
	for _, s := range Schemes {
		x := Scheme{Scheme: s}
		choice := readString(registry.CURRENT_USER, `Software\Microsoft\Windows\Shell\Associations\UrlAssociations\`+s+`\UserChoice`, "ProgId")
		switch {
		case choice == progID:
			x.Skiff, x.Handler = true, "Skiff"
		case choice != "":
			// the application chosen in the settings
			cmd := readString(registry.CLASSES_ROOT, choice+`\shell\open\command`, "")
			x.Skiff = isSkiff(cmd)
			x.Handler = readString(registry.CLASSES_ROOT, choice+`\Application`, "ApplicationName")
			if x.Handler == "" {
				x.Handler = exeName(cmd)
			}
			if x.Handler == "" {
				x.Handler = choice
			}
		default:
			cmd := readString(registry.CLASSES_ROOT, s+`\shell\open\command`, "")
			x.Skiff = isSkiff(cmd)
			if cmd != "" {
				x.Handler = exeName(cmd)
			}
		}
		if x.Skiff {
			x.Handler = "Skiff"
		}
		st.Schemes = append(st.Schemes, x)
	}
	return st
}

func setValues(path string, values map[string]string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	for n, v := range values {
		if err := k.SetStringValue(n, v); err != nil {
			return err
		}
	}
	return nil
}

func register(exe string, icon []byte) error {
	if exe == "" {
		return errors.New("cannot tell where this program is")
	}
	icn := `"` + exe + `",0`
	cmd := command(exe)
	// the application, for the default applications of the settings
	if err := setValues(classes+progID, map[string]string{"": "Skiff URL", "URL Protocol": ""}); err != nil {
		return err
	}
	setValues(classes+progID+`\DefaultIcon`, map[string]string{"": icn})
	setValues(classes+progID+`\Application`, map[string]string{"ApplicationName": "Skiff", "ApplicationIcon": icn})
	if err := setValues(classes+progID+`\shell\open\command`, map[string]string{"": cmd}); err != nil {
		return err
	}
	setValues(capsPath, map[string]string{"ApplicationName": "Skiff", "ApplicationDescription": "SFTP, SCP, FTP, WebDAV and S3 client"})
	assoc := map[string]string{}
	for _, s := range Schemes {
		assoc[s] = progID
		// the protocol itself, used when no application was chosen
		if err := setValues(classes+s, map[string]string{"": "URL:" + strings.ToUpper(s), "URL Protocol": ""}); err != nil {
			return err
		}
		setValues(classes+s+`\DefaultIcon`, map[string]string{"": icn})
		if err := setValues(classes+s+`\shell\open\command`, map[string]string{"": cmd}); err != nil {
			return err
		}
	}
	setValues(capsPath+`\URLAssociations`, assoc)
	setValues(`Software\RegisteredApplications`, map[string]string{"Skiff": capsPath})
	notifyShell()
	return nil
}

// deleteTree removes a key and everything under it.
func deleteTree(root registry.Key, path string) error {
	k, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	subs, _ := k.ReadSubKeyNames(-1)
	k.Close()
	for _, s := range subs {
		if err := deleteTree(root, path+`\`+s); err != nil {
			return err
		}
	}
	err = registry.DeleteKey(root, path)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

func remove(exe string) error {
	for _, s := range Schemes {
		// only the protocols that start Skiff
		if isSkiff(readString(registry.CURRENT_USER, classes+s+`\shell\open\command`, "")) {
			if err := deleteTree(registry.CURRENT_USER, classes+s); err != nil {
				return err
			}
		}
	}
	if err := deleteTree(registry.CURRENT_USER, classes+progID); err != nil {
		return err
	}
	deleteTree(registry.CURRENT_USER, `Software\Skiff`)
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\RegisteredApplications`, registry.SET_VALUE); err == nil {
		k.DeleteValue("Skiff")
		k.Close()
	}
	notifyShell()
	return nil
}

func openSettings() error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", "ms-settings:defaultapps?registeredAppUser=Skiff").Start()
}

// notifyShell tells Explorer that the associations changed.
func notifyShell() {
	const shcneAssocChanged, shcnfIDList = 0x08000000, 0
	proc := windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify")
	proc.Call(shcneAssocChanged, shcnfIDList, 0, 0)
}
