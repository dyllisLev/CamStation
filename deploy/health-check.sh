#!/bin/bash
# deploy/health-check.sh
curl -sf http://127.0.0.1:8000/api/system/health > /dev/null 2>&1
