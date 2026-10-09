package godebug

func setUpdate(update func(string, string))                     {}
func registerMetric(name string, read func() uint64)            {}
func setNewIncNonDefault(newIncNonDefault func(string) func()) {}
