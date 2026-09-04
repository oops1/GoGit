package ops

import (
	"io/fs"
	"os"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

var (
	fsOpenRoot     = os.OpenRoot
	fsRootOpen     = (*os.Root).Open
	fsRootOpenFile = (*os.Root).OpenFile
	fsRootLstat    = (*os.Root).Lstat
	fsRootReadFile = (*os.Root).ReadFile
	fsRootReadlink = (*os.Root).Readlink
	fsRootRemove   = (*os.Root).Remove
	fsRootSymlink  = (*os.Root).Symlink
	fsRootMkdirAll = (*os.Root).MkdirAll
	fsRootRename   = (*os.Root).Rename
)

var (
	odbOpen  = odb.Open
	refsOpen = refs.Open
)

var (
	dbPutObject   = (*odb.DB).PutObject
	dbPut         = (*odb.DB).Put
	dbTree        = (*odb.DB).Tree
	dbCommit      = (*odb.DB).Commit
	dbPeel        = (*odb.DB).Peel
	txUpdate      = (*refs.Transaction).Update
	txDelete      = (*refs.Transaction).Delete
	txSetSymbolic = (*refs.Transaction).SetSymbolic
	txDetach      = (*refs.Transaction).Detach
)

var (
	idxWrite    = (*index.Index).Write
	fsFileClose = (*os.File).Close
	direntInfo  = fs.DirEntry.Info
	hashSum     = hash.Sum
)
