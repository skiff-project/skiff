package core

import "testing"

func TestParseDockerPS(t *testing.T) {
	// docker ps -a --format '{{json .}}': strings, labels with commas in values
	docker := `{"Image":"nginx:stable","Labels":"org.opencontainers.image.description=a, b and c,com.docker.compose.project=boutique,com.docker.compose.service=web","Names":"boutique-web-1","Ports":"0.0.0.0:8080->80/tcp","State":"running","Status":"Up 5 minutes"}
{"Image":"redis","Labels":"","Names":"cache","Ports":"","State":"exited","Status":"Exited (0) 2 days ago"}
`
	got := parseDockerPS(docker)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if w := got[0]; w.Name != "boutique-web-1" || !w.Running || w.State != "running" || w.Project != "boutique" || w.Ports != "0.0.0.0:8080->80/tcp" {
		t.Errorf("docker web %+v", w)
	}
	if c := got[1]; c.Running || c.State != "exited" || c.Project != "" {
		t.Errorf("docker cache %+v", c)
	}
	// podman ps -a --format '{{json .}}': lists and maps
	podman := `{"Names":["api"],"Image":"docker.io/library/python:3","State":"running","Status":"Up 3 hours","Labels":{"com.docker.compose.project":"shop"},"Ports":[{"host_ip":"","host_port":8000,"container_port":8000,"protocol":"tcp"}]}`
	got = parseDockerPS(podman)
	if len(got) != 1 || got[0].Name != "api" || !got[0].Running || got[0].Project != "shop" || got[0].Ports != "0.0.0.0:8000->8000/tcp" {
		t.Errorf("podman %+v", got)
	}
	// podman ps -a --format json: an array
	got = parseDockerPS(`[{"Names":["db"],"Image":"postgres","State":"exited","Status":"Exited (1) 1 minute ago"}]`)
	if len(got) != 1 || got[0].Name != "db" || got[0].Running || got[0].State != "exited" {
		t.Errorf("podman array %+v", got)
	}
}

func TestParsePodsState(t *testing.T) {
	got, err := parsePods([]byte(`{"items":[{"metadata":{"name":"api-1","namespace":"prod"},"spec":{"containers":[{"name":"api","image":"api:1"}]},"status":{"phase":"Pending"}}]}`))
	if err != nil || len(got) != 1 || got[0].State != "pending" || got[0].Running || got[0].Image != "api:1" {
		t.Errorf("%+v %v", got, err)
	}
}
