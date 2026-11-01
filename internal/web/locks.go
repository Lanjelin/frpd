package web

// lockInstance serializes proxy mutations per frpc instance and returns the
// unlock func: toggle is a read-modify-write (GetProxy then UpdateProxy), so
// without it a concurrent update or delete from this dashboard could be lost.
// Reads stay lock-free. The lock map is read-only after New.
func (s *Server) lockInstance(name string) func() {
	lock := s.instanceLocks[name]
	lock.Lock()
	return lock.Unlock
}
