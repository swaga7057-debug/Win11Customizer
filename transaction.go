package main

import (
	"errors"
	"fmt"
)

type registryPlan struct {
	op      operation
	enabled bool
}

// executeRegistryTransaction is the single mutation seam for registry-backed
// settings. It persists an exact snapshot before the first write, verifies
// every result and restores all values in reverse order on any failure.
func (a *app) executeRegistryTransaction(plans []registryPlan) ([]backupValue, error) {
	if len(plans) == 0 {
		return nil, nil
	}
	before := make([]backupValue, 0, len(plans))
	for _, plan := range plans {
		value, err := readBackupValue(plan.op)
		if err != nil {
			return nil, err
		}
		before = append(before, value)
		a.rememberValue(plan.op, value)
	}
	if err := a.saveBackup(); err != nil {
		return nil, err
	}
	for index, plan := range plans {
		if err := applyOperation(plan.op, plan.enabled); err != nil {
			return nil, transactionError(err, restoreValues(before[:index+1]))
		}
		matches, err := operationMatches(plan.op, plan.enabled)
		if err != nil || !matches {
			if err == nil {
				err = errors.New("Windows не подтвердила изменение")
			}
			return nil, transactionError(err, restoreValues(before[:index+1]))
		}
	}
	return before, nil
}

func transactionError(applyErr, rollbackErr error) error {
	if rollbackErr != nil {
		return fmt.Errorf("%v; автоматический откат: %w", applyErr, rollbackErr)
	}
	return fmt.Errorf("%w (изменения отменены)", applyErr)
}
