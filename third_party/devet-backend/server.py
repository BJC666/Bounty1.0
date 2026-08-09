"""DeVET Backend Server — FastAPI entry point."""
import sys
import os

# Add vet-repro to path so we can import existing modules
VET_REPRO = os.path.join(os.path.dirname(__file__), "..", "vet-repro")
if VET_REPRO not in sys.path:
    sys.path.insert(0, VET_REPRO)

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.staticfiles import StaticFiles

from api_routes import router

app = FastAPI(title="DeVET System API", version="1.0.0")

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)

app.include_router(router, prefix="/api")


@app.get("/")
def root():
    return {"service": "DeVET System API", "status": "running"}


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="127.0.0.1", port=8765)

