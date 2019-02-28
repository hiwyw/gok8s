package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/zdnscloud/gok8s/client/config"
	"github.com/zdnscloud/gok8s/exec"
)

func main() {
	r, w := io.Pipe()
	cmd := exec.Cmd{
		Path:   "/bin/sh",
		Stdin:  r,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}

	pod := exec.Pod{
		Namespace:          "default",
		Name:               "kubectl-ben",
		Image:              "rancher/rancher-agent:v2.1.6",
		ServiceAccountName: "cluster-readonly",
	}

	cfg, err := config.GetConfig()
	if err != nil {
		panic("get cfg failed:" + err.Error())
	}

	e, err := exec.New(cfg)
	if err != nil {
		panic("create executor failed:" + err.Error())
	}

	go func() {
		defer w.Close()
		_, err = io.Copy(w, os.Stdin)
		if err != nil {
			fmt.Printf("cannot copy from stdin: %v", err)
		}
	}()

	if err := e.RunCmd(pod, cmd, 30*time.Second); err != nil {
		fmt.Printf("run cmd failed:%s\n", err.Error())
	}
}
