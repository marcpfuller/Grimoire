package ghostwriter

// SetOplogsForTest injects a pre-built oplog list into c, bypassing Init.
// Used by external tests to satisfy the PushCommand Init guard without a real
// GW round-trip. PushCommand routes via cmd.OplogID directly, so only the
// non-nil state of c.oplogs matters for the guard; the slice contents matter
// for Oplogs() assertions.
func SetOplogsForTest(c *Client, oplogs []OplogEntry) {
	c.oplogs = oplogs
}

// SetMultiClientForTest wires a pre-built byOplog routing table and oplog list
// into mc, bypassing Init. Useful for MultiClient unit tests.
func SetMultiClientForTest(mc *MultiClient, byOplog map[int]*Client, oplogs []OplogEntry) {
	mc.byOplog = byOplog
	mc.allOplogs = oplogs
	mc.clients = make([]*Client, 0, len(byOplog))
	seen := map[*Client]bool{}
	for _, c := range byOplog {
		if !seen[c] {
			mc.clients = append(mc.clients, c)
			seen[c] = true
		}
	}
}
