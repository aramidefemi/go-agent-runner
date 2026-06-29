package executor

import "errors"

var ErrLockHeld = errors.New("runner lock held by active process")
