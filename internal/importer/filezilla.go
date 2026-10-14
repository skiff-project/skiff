package importer

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"strconv"
	"strings"

	"skiff/internal/config"
)

type fzServer struct {
	Host      string `xml:"Host"`
	Port      int    `xml:"Port"`
	Protocol  int    `xml:"Protocol"`
	User      string `xml:"User"`
	Pass      struct {
		Value    string `xml:",chardata"`
		Encoding string `xml:"encoding,attr"`
	} `xml:"Pass"`
	Keyfile    string `xml:"Keyfile"`
	Name       string `xml:"Name"`
	Comments   string `xml:"Comments"`
	Colour     int    `xml:"Colour"`
	LocalDir   string `xml:"LocalDir"`
	RemoteDir  string `xml:"RemoteDir"`
	PasvMode   string `xml:"PasvMode"`
	EncodingType string `xml:"EncodingType"`
	CustomEncoding string `xml:"CustomEncoding"`
}

type fzFolder struct {
	Name    string     `xml:",chardata"`
	Servers []fzServer `xml:"Server"`
	Folders []fzFolder `xml:"Folder"`
}

type fzRoot struct {
	Servers struct {
		Servers []fzServer `xml:"Server"`
		Folders []fzFolder `xml:"Folder"`
	} `xml:"Servers"`
}

// fzRemoteDir decodes FileZilla's "1 0 4 home 4 user" path encoding.
func fzRemoteDir(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// type SP prefixlen [SP prefix] {SP len SP segment}
	i := strings.IndexByte(s, ' ')
	if i < 0 {
		return ""
	}
	rest := s[i+1:]
	j := strings.IndexByte(rest, ' ')
	if j < 0 {
		return "/"
	}
	plen, _ := strconv.Atoi(rest[:j])
	rest = rest[j+1:]
	if plen > 0 && len(rest) >= plen {
		rest = strings.TrimPrefix(rest[plen:], " ")
	}
	var segs []string
	for rest != "" {
		k := strings.IndexByte(rest, ' ')
		if k < 0 {
			break
		}
		n, err := strconv.Atoi(rest[:k])
		if err != nil || len(rest) < k+1+n {
			break
		}
		segs = append(segs, rest[k+1:k+1+n])
		rest = strings.TrimPrefix(rest[k+1+n:], " ")
	}
	return "/" + strings.Join(segs, "/")
}

func fzItem(sv fzServer, folder string) (Item, bool) {
	s := config.NewSite()
	s.Folder = folder
	s.Name = strings.TrimSpace(sv.Name)
	s.Host = sv.Host
	s.User = sv.User
	s.Notes = sv.Comments
	s.KeyFile = sv.Keyfile
	s.LocalDir = sv.LocalDir
	s.RemoteDir = fzRemoteDir(sv.RemoteDir)
	switch sv.Protocol {
	case 1:
		s.Protocol = config.ProtoSFTP
	case 3:
		s.Protocol, s.FTPS = config.ProtoFTP, "implicit"
	case 4:
		s.Protocol, s.FTPS = config.ProtoFTP, "explicit"
	case 0, 6:
		s.Protocol = config.ProtoFTP
	case 5:
		s.Protocol = config.ProtoS3
		s.TLS = true
	case 10, 11:
		s.Protocol, s.TLS = config.ProtoWebDAV, true
	default:
		return Item{}, false
	}
	if sv.Port != 0 && sv.Port != config.DefaultPort(&s) {
		s.Port = sv.Port
	}
	if sv.PasvMode == "MODE_ACTIVE" {
		s.FTPActive = true
	}
	if sv.EncodingType == "Custom" && sv.CustomEncoding != "" {
		s.Encoding = sv.CustomEncoding
	}
	it := Item{Site: s}
	if sv.Pass.Value != "" {
		if sv.Pass.Encoding == "base64" {
			if b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sv.Pass.Value)); err == nil {
				it.Password = string(b)
			}
		} else if sv.Pass.Encoding == "crypt" {
			it.Note = "password is protected by a FileZilla master password and was not imported"
		} else {
			it.Password = sv.Pass.Value
		}
	}
	return it, s.Host != ""
}

func parseFileZilla(data []byte) ([]Item, error) {
	var root fzRoot
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	var out []Item
	var walk func(f fzFolder, prefix string)
	walk = func(f fzFolder, prefix string) {
		name := strings.TrimSpace(f.Name)
		path := name
		if prefix != "" {
			path = prefix + "/" + name
		}
		for _, sv := range f.Servers {
			if it, ok := fzItem(sv, path); ok {
				out = append(out, it)
			}
		}
		for _, sub := range f.Folders {
			walk(sub, path)
		}
	}
	for _, sv := range root.Servers.Servers {
		if it, ok := fzItem(sv, ""); ok {
			out = append(out, it)
		}
	}
	for _, f := range root.Servers.Folders {
		walk(f, "")
	}
	if len(out) == 0 {
		return nil, errors.New("no FileZilla sites found")
	}
	return out, nil
}
