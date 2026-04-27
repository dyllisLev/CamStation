import pytest
from httpx import AsyncClient

pytestmark = pytest.mark.anyio


async def test_list_layouts_empty(client):
    r = await client.get("/api/layouts")
    assert r.status_code == 200
    assert r.json() == []


async def test_create_layout(client):
    payload = {
        "name": "테스트",
        "data": [{"i": "cam1", "x": 0, "y": 0, "w": 6, "h": 4}],
    }
    r = await client.post("/api/layouts", json=payload)
    assert r.status_code == 201
    body = r.json()
    assert body["name"] == "테스트"
    assert body["id"] is not None
    assert body["data"][0]["i"] == "cam1"


async def test_create_layout_empty_name_fails(client):
    r = await client.post("/api/layouts", json={"name": "  ", "data": []})
    assert r.status_code == 422


async def test_list_returns_created(client):
    await client.post("/api/layouts", json={"name": "A", "data": []})
    await client.post("/api/layouts", json={"name": "B", "data": []})
    r = await client.get("/api/layouts")
    names = [l["name"] for l in r.json()]
    assert "A" in names and "B" in names


async def test_update_layout(client):
    r = await client.post("/api/layouts", json={"name": "원본", "data": []})
    layout_id = r.json()["id"]
    r2 = await client.put(f"/api/layouts/{layout_id}", json={"name": "수정됨"})
    assert r2.status_code == 200
    assert r2.json()["name"] == "수정됨"


async def test_update_data(client):
    r = await client.post("/api/layouts", json={
        "name": "test",
        "data": [{"i": "cam1", "x": 0, "y": 0, "w": 3, "h": 2}],
    })
    layout_id = r.json()["id"]
    new_data = [{"i": "cam1", "x": 6, "y": 0, "w": 6, "h": 4}]
    r2 = await client.put(f"/api/layouts/{layout_id}", json={"data": new_data})
    assert r2.json()["data"][0]["x"] == 6


async def test_update_nonexistent_returns_404(client):
    r = await client.put("/api/layouts/nonexistent", json={"name": "x"})
    assert r.status_code == 404


async def test_delete_layout(client):
    r = await client.post("/api/layouts", json={"name": "삭제용", "data": []})
    layout_id = r.json()["id"]
    r2 = await client.delete(f"/api/layouts/{layout_id}")
    assert r2.status_code == 204
    r3 = await client.get("/api/layouts")
    ids = [l["id"] for l in r3.json()]
    assert layout_id not in ids


async def test_delete_nonexistent_returns_404(client):
    r = await client.delete("/api/layouts/nonexistent")
    assert r.status_code == 404
