package ghostwriter

// SetOplogByProjectForTest registers a projectID→oplogID mapping on c, bypassing
// Init. Used by external tests to inject known IDs without a discovery round-trip.
func SetOplogByProjectForTest(c *Client, projectID, oplogID int) {
	if c.oplogByProject == nil {
		c.oplogByProject = make(map[int]int)
	}
	c.oplogByProject[projectID] = oplogID
}
