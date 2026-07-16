package viewerinstall

import "errors"

// TransactionOwned probes the same machine-wide transaction primitive without
// retaining ownership when it is available.
func TransactionOwned(layout Layout) (bool, error) {
	owner, err := Acquire(layout)
	if errors.Is(err, ErrUpdateOwned) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if err := owner.Close(); err != nil {
		return false, err
	}
	return false, nil
}
