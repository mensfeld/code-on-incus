package container

// ContainerManager is the interface implemented by *Manager.
// Defining it here lets other packages (session, cli) accept a fake/stub
// in tests without needing a live Incus installation.
type ContainerManager interface {
	Launch(image string, ephemeral bool, pool string) error
	Stop(force bool) error
	Delete(force bool) error
	Running() (bool, error)
	Exists() (bool, error)
	Start() error
	MountDisk(name, source, path string, shift, readonly bool) error
	AddProxyDevice(name, connect, listen string, uid, gid int) error
	RemoveDevice(name string) error
	SetTmpfsSize(size string) error
	GetWorkspacePath() string
	Exec(args ...string) error
	ExecArgs(commandArgs []string, opts ExecCommandOptions) error
	ExecArgsCapture(commandArgs []string, opts ExecCommandOptions) (string, error)
	ExecCommand(command string, opts ExecCommandOptions) (string, error)
	PushFile(source, destination string) error
	PullDirectory(containerPath, localPath string) error
	PushDirectory(localPath, containerPath string) error
	Chown(path string, uid, gid int) error
	DirExists(path string) (bool, error)
	FileExists(path string) (bool, error)
	CreateFile(containerPath, content string) error
	ExecHostCommand(command string, capture bool) (string, error)
	CreateSnapshot(name string, stateful bool) error
	ListSnapshots() ([]SnapshotInfo, error)
	RestoreSnapshot(name string, stateful bool) error
	DeleteSnapshot(name string) error
	SnapshotExists(name string) (bool, error)
	GetSnapshotInfo(name string) (*SnapshotInfo, error)
}

// Compile-time assertion: *Manager must satisfy ContainerManager.
var _ ContainerManager = (*Manager)(nil)
