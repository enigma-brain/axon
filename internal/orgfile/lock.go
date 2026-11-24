package orgfile

// lock takes the exclusive lock on changing the stored config files,
// waiting while another process or goroutine holds it. The returned function
// releases it.
func (s Store) lock() (unlock func(), err error) { return s.dir.Lock() }
