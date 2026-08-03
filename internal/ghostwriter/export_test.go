package ghostwriter

// SetOplogByProjectForTest initializes the oplogByProject map on c and
// registers a projectID→oplogID mapping. Used by external tests to satisfy
// the PushCommand Init guard without a real GW round-trip. PushCommand now
// routes via cmd.OplogID directly, so only the map's non-nil state matters.
func SetOplogByProjectForTest(c *Client, projectID, oplogID int) {
	if c.oplogByProject == nil {
		c.oplogByProject = make(map[int]int)
	}
	c.oplogByProject[projectID] = oplogID
}
