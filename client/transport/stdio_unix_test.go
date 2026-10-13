//go:build unix

package transport

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStdio_WithCommandFunc(t *testing.T) {
	called := false
	tmpDir := t.TempDir()
	chrootDir := filepath.Join(tmpDir, "sandbox-root")
	err := os.MkdirAll(chrootDir, 0o755)
	require.NoError(t, err, "failed to create chroot dir")

	fakeCmdFunc := func(ctx context.Context, command string, args []string, env []string) (*exec.Cmd, error) {
		called = true

		// Override the args inside our command func.
		cmd := exec.CommandContext(ctx, command, "bonjour")

		// Simulate some security-related settings for test purposes.
		cmd.Env = []string{"PATH=/usr/bin", "NODE_ENV=production"}
		cmd.Dir = tmpDir

		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: 1001,
				Gid: 1001,
			},
			Chroot: chrootDir,
		}

		return cmd, nil
	}

	stdio := NewStdioWithOptions(
		"echo",
		[]string{"foo=bar"},
		[]string{"hello"},
		WithCommandFunc(fakeCmdFunc),
	)
	require.NotNil(t, stdio)
	require.NotNil(t, stdio.cmdFunc)

	// Manually call the cmdFunc passing the same values as in spawnCommand.
	cmd, err := stdio.cmdFunc(t.Context(), "echo", nil, []string{"hello"})
	require.NoError(t, err)
	require.True(t, called)
	require.NotNil(t, cmd)
	require.NotNil(t, cmd.SysProcAttr)
	require.Equal(t, chrootDir, cmd.SysProcAttr.Chroot)
	require.Equal(t, tmpDir, cmd.Dir)
	require.Equal(t, uint32(1001), cmd.SysProcAttr.Credential.Uid)
	require.Equal(t, "echo", filepath.Base(cmd.Path))
	require.Len(t, cmd.Args, 2)
	require.Contains(t, cmd.Args, "bonjour")
	require.Len(t, cmd.Env, 2)
	require.Contains(t, cmd.Env, "PATH=/usr/bin")
	require.Contains(t, cmd.Env, "NODE_ENV=production")
}
