package viewerservice

func isInteractivePeer(sessionID uint32, tokenInteractive, sessionActive bool) bool {
	return sessionID != 0 && (tokenInteractive || sessionActive)
}
