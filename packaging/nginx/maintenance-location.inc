# Short maintenance response used only while the single-active backend handoff runs.
location / {
    add_header Retry-After 60 always;
    default_type text/plain;
    return 503 "CamStation maintenance\n";
}
