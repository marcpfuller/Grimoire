package ghostwriter

// SetOplogIDForTest sets the resolved oplog ID on c, bypassing Init.
// Used by external tests to inject a known ID without a discovery round-trip.
func SetOplogIDForTest(c *Client, id int) {
	c.resolvedOplogID = id
}
