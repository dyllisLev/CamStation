package main

import (
	"camstation/internal/store"
	"camstation/internal/stream"
)

func annotateCameraRuntimeStatus(cameras []store.Camera, status stream.Status) {
	if len(status.Streams) == 0 {
		return
	}
	for i := range cameras {
		running := false
		for j := range cameras[i].Streams {
			if runtime, ok := status.Streams[cameras[i].Streams[j].Go2RTCStreamName]; ok {
				cameras[i].Streams[j].State = runtime.State
				if streamRuntimeRunning(runtime) {
					running = true
				}
			}
		}
		for _, streamName := range []string{cameras[i].RecordingStreamName, cameras[i].LiveStreamName, cameras[i].StreamName} {
			if runtime, ok := status.Streams[streamName]; ok && streamRuntimeRunning(runtime) {
				running = true
			}
		}
		if running {
			cameras[i].State = "streaming"
		}
	}
}

func streamRuntimeRunning(runtime stream.StreamRuntime) bool {
	return runtime.State == "running" && runtime.ProducerCount > 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func cameraByStream(cameras []store.Camera, streamName string) (store.Camera, bool) {
	for _, camera := range cameras {
		if camera.StreamName == streamName || camera.RecordingStreamName == streamName || camera.LiveStreamName == streamName {
			return camera, true
		}
		for _, stream := range camera.Streams {
			if stream.Go2RTCStreamName == streamName {
				return camera, true
			}
		}
	}
	return store.Camera{}, false
}
