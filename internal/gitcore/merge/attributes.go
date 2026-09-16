package merge

type Driver int

const (
	DriverText Driver = iota
	DriverBinary
	DriverUnion
	DriverExternal
)

type PathAttributes struct {
	Driver     Driver
	Name       string
	MarkerSize int
}

type WarningKind int

const (
	WarningExternalDriver WarningKind = iota
	WarningRenameLimit
	WarningDirectoryRenameSplit
	WarningDirectoryRenameCollision
	WarningDirectoryRenameInTheWay
)

type Warning struct {
	Kind    WarningKind
	Path    string
	Driver  string
	Needed  int
	Sources string
}

func (w Warning) Unclean() bool {
	return w.Kind >= WarningDirectoryRenameSplit
}

func (o TreeOptions) attributesFor(path string) PathAttributes {
	if o.Attributes == nil {
		return PathAttributes{}
	}
	return o.Attributes(path, o.Depth > 0)
}

func (o TreeOptions) warn(w Warning) {
	*o.warnings = append(*o.warnings, w)
}

func (o TreeOptions) fileOptionsFor(attrs PathAttributes) Options {
	file := o.File
	if attrs.MarkerSize > 0 {
		file.MarkerSize = attrs.MarkerSize
	}
	file.MarkerSize = file.markerSize() + o.extraMarkers + 2*o.Depth
	file.Union = attrs.Driver == DriverUnion
	return file
}
