/*
Copyright 2018 Google LLC
Copyright 2022 David Gageot

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vscode

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/dgageot/demoit/files"
	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/go-connections/nat"
	"golang.org/x/term"
)

const (
	Port          = 18080
	dockerImage   = "codercom/code-server:4.9.1@sha256:e713ed9a211b4ef8be4ec01ce9003aeeed4e1981c563c224f42635feccb08800"
	containerName = "demoit-vscode"
)

var (
	defaultFlags = []string{"--auth=none", "--disable-telemetry", "--disable-update-check", "--force"}
	startOnce    sync.Once
)

func Start() {
	startOnce.Do(func() {
		if err := startVsCodeServer(context.Background()); err != nil {
			log.Println(err)
		}
	})
}

func startVsCodeServer(ctx context.Context) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("can't get current directory: %w", err)
	}

	user, err := user.Current()
	if err != nil {
		return fmt.Errorf("can't get current user: %w", err)
	}

	client, err := newDockerlient(ctx)
	if err != nil {
		return fmt.Errorf("docker is not available: %w", err)
	}

	// Ignore error
	_ = client.ContainerRemove(ctx, containerName, types.ContainerRemoveOptions{Force: true})

	rc, err := client.ImagePull(ctx, dockerImage, types.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("unable to initiate vscode image pull: %w", err)
	}
	defer rc.Close()
	if err := streamDockerMessages(rc); err != nil {
		return fmt.Errorf("unable to pull vscode image: %w", err)
	}

	userHomeDir, err := os.UserHomeDir()

	if err != nil {
		return fmt.Errorf("unable to get user home directory: %w", err)
	}

	body, err := client.ContainerCreate(ctx, &containertypes.Config{
		Image: dockerImage,
		User:  fmt.Sprintf("%s:%s", user.Uid, user.Gid),
		Cmd:   defaultFlags,
		ExposedPorts: nat.PortSet{
			nat.Port("8080/tcp"): struct{}{},
		},
		Env: []string{
			"DOCKER_USER=" + user.Username,
		},
	}, &containertypes.HostConfig{
		Binds: []string{
			filepath.Join(cwd, files.Root) + ":/app",
			filepath.Join(userHomeDir, "/.config") + ":/home/coder/.config",
		},
		PortBindings: nat.PortMap{
			nat.Port("8080/tcp"): []nat.PortBinding{{HostPort: strconv.Itoa(Port)}},
		},
	}, nil, nil, containerName)
	if err != nil {
		return fmt.Errorf("unable to create a docker container for vscode: %w", err)
	}

	if err := client.ContainerStart(ctx, body.ID, types.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("unable to start a docker container for vscode: %w", err)
	}

	return nil
}

func newDockerlient(ctx context.Context) (client.CommonAPIClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("error getting docker client: %w", err)
	}
	cli.NegotiateAPIVersion(ctx)

	return cli, nil
}

func streamDockerMessages(src io.Reader) error {
	dst := os.Stdout
	termFd, isTerm := isTerminal(dst)
	return jsonmessage.DisplayJSONMessagesStream(src, dst, termFd, isTerm, nil)
}

func isTerminal(w io.Writer) (uintptr, bool) {
	type descriptor interface {
		Fd() uintptr
	}

	if f, ok := w.(descriptor); ok {
		termFd := f.Fd()
		isTerm := term.IsTerminal(int(termFd))
		return termFd, isTerm
	}

	return 0, false
}
