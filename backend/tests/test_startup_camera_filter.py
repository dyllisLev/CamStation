def test_startup_camera_lists_ignore_disabled_go2rtc_streams():
    from main import _startup_camera_lists

    all_keys = [
        "camera-yard",
        "camera-yard_sub",
        "camera-site-1",
        "camera-site-1_sub",
        "camera-site-2",
        "camera-site-2_sub",
    ]
    enabled_ids = ["camera-yard", "camera-site-1"]

    cam_ids, sub_cam_ids = _startup_camera_lists(all_keys, enabled_ids)

    assert cam_ids == ["camera-yard", "camera-site-1"]
    assert sub_cam_ids == ["camera-yard", "camera-site-1"]
    assert "camera-site-2" not in cam_ids
    assert "camera-site-2" not in sub_cam_ids


def test_startup_camera_lists_falls_back_to_config_when_go2rtc_unavailable():
    from main import _startup_camera_lists

    cam_ids, sub_cam_ids = _startup_camera_lists([], ["camera-yard", "camera-site-1"])

    assert cam_ids == ["camera-yard", "camera-site-1"]
    assert sub_cam_ids == []
