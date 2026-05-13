import uuid
import time
import json
import aiosqlite
from fastapi import APIRouter, HTTPException
from config import get_db_path
from models import LayoutProfile, CreateLayoutRequest, UpdateLayoutRequest

router = APIRouter(prefix="/api/layouts", tags=["layouts"])


@router.get("", response_model=list[LayoutProfile])
async def list_layouts():
    async with aiosqlite.connect(get_db_path()) as db:
        db.row_factory = aiosqlite.Row
        cursor = await db.execute(
            "SELECT id, name, data, timeline_collapsed, grid_cols, grid_rows, created_at, updated_at FROM layouts ORDER BY updated_at DESC"
        )
        rows = await cursor.fetchall()
    return [
        LayoutProfile(
            id=row["id"],
            name=row["name"],
            data=json.loads(row["data"]),
            timeline_collapsed=bool(row["timeline_collapsed"]),
            grid_cols=row["grid_cols"],
            grid_rows=row["grid_rows"],
            created_at=row["created_at"],
            updated_at=row["updated_at"],
        )
        for row in rows
    ]


@router.post("", response_model=LayoutProfile, status_code=201)
async def create_layout(req: CreateLayoutRequest):
    if not req.name.strip():
        raise HTTPException(status_code=422, detail="name cannot be empty")
    layout_id = str(uuid.uuid4())
    now = int(time.time())
    data_json = json.dumps([item.model_dump(exclude_none=True) for item in req.data])
    async with aiosqlite.connect(get_db_path()) as db:
        await db.execute(
            "INSERT INTO layouts (id, name, data, timeline_collapsed, grid_cols, grid_rows, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
            (layout_id, req.name.strip(), data_json, int(req.timeline_collapsed), req.grid_cols, req.grid_rows, now, now),
        )
        await db.commit()
    return LayoutProfile(
        id=layout_id,
        name=req.name.strip(),
        data=req.data,
        timeline_collapsed=req.timeline_collapsed,
        grid_cols=req.grid_cols,
        grid_rows=req.grid_rows,
        created_at=now,
        updated_at=now,
    )


@router.put("/{layout_id}", response_model=LayoutProfile)
async def update_layout(layout_id: str, req: UpdateLayoutRequest):
    async with aiosqlite.connect(get_db_path()) as db:
        db.row_factory = aiosqlite.Row
        cursor = await db.execute(
            "SELECT id, name, data, timeline_collapsed, grid_cols, grid_rows, created_at FROM layouts WHERE id = ?", (layout_id,)
        )
        row = await cursor.fetchone()
        if not row:
            raise HTTPException(status_code=404, detail="Layout not found")
        now = int(time.time())
        new_name = req.name.strip() if req.name is not None else row["name"]
        new_data = (
            json.dumps([item.model_dump(exclude_none=True) for item in req.data])
            if req.data is not None
            else row["data"]
        )
        new_tc = req.timeline_collapsed if req.timeline_collapsed is not None else bool(row["timeline_collapsed"])
        new_grid_cols = req.grid_cols if req.grid_cols is not None else row["grid_cols"]
        new_grid_rows = req.grid_rows if req.grid_rows is not None else row["grid_rows"]
        await db.execute(
            "UPDATE layouts SET name = ?, data = ?, timeline_collapsed = ?, grid_cols = ?, grid_rows = ?, updated_at = ? WHERE id = ?",
            (new_name, new_data, int(new_tc), new_grid_cols, new_grid_rows, now, layout_id),
        )
        await db.commit()
    return LayoutProfile(
        id=layout_id,
        name=new_name,
        data=json.loads(new_data),
        timeline_collapsed=new_tc,
        grid_cols=new_grid_cols,
        grid_rows=new_grid_rows,
        created_at=row["created_at"],
        updated_at=now,
    )


@router.delete("/{layout_id}", status_code=204)
async def delete_layout(layout_id: str):
    async with aiosqlite.connect(get_db_path()) as db:
        cursor = await db.execute("DELETE FROM layouts WHERE id = ?", (layout_id,))
        await db.commit()
        if cursor.rowcount == 0:
            raise HTTPException(status_code=404, detail="Layout not found")
