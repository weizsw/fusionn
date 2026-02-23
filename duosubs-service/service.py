#!/usr/bin/env python3
"""
DuoSubs HTTP Service
Runs on host to leverage Metal GPU acceleration.
Called by fusionn container via HTTP.
"""

import os
from pathlib import Path
from typing import Optional

from fastapi import FastAPI
from pydantic import BaseModel
import uvicorn

# Import DuoSubs Python API
from duosubs import MergeArgs, run_merge_pipeline

app = FastAPI(title="DuoSubs Service", version="1.0.0")


class MergeRequest(BaseModel):
    """Request to merge subtitles"""
    primary_path: str       # Path to primary subtitle (Chinese)
    secondary_path: str     # Path to secondary subtitle (English)
    output_dir: str        # Directory to write output
    container_prefix: str = "/data"  # Container path prefix
    host_prefix: str       # Host path prefix (configured at startup)


class MergeResponse(BaseModel):
    """Response from merge operation"""
    success: bool
    output_path: Optional[str] = None
    error: Optional[str] = None


def translate_path(container_path: str, container_prefix: str, host_prefix: str) -> str:
    """Translate container path to host path"""
    if not container_path.startswith(container_prefix):
        return container_path
    
    relative = container_path[len(container_prefix):].lstrip("/")
    return str(Path(host_prefix) / relative)


@app.get("/health")
async def health():
    """Health check endpoint"""
    return {"status": "healthy", "service": "duosubs"}


@app.post("/merge", response_model=MergeResponse)
async def merge_subtitles(request: MergeRequest):
    """
    Merge two subtitle files using duosubs Python API.
    Paths are translated from container paths to host paths.
    """
    
    # Get host prefix from environment
    host_prefix = os.getenv("HOST_MEDIA_PATH", request.host_prefix)
    
    # Translate paths
    primary_host = translate_path(request.primary_path, request.container_prefix, host_prefix)
    secondary_host = translate_path(request.secondary_path, request.container_prefix, host_prefix)
    output_dir_host = translate_path(request.output_dir, request.container_prefix, host_prefix)
    
    # Validate input files exist
    if not Path(primary_host).exists():
        return MergeResponse(
            success=False,
            error=f"Primary subtitle not found: {primary_host}"
        )
    
    if not Path(secondary_host).exists():
        return MergeResponse(
            success=False,
            error=f"Secondary subtitle not found: {secondary_host}"
        )
    
    # Create output directory if needed
    Path(output_dir_host).mkdir(parents=True, exist_ok=True)
    
    try:
        # Determine output basename (use primary filename without extension)
        basename = Path(primary_host).stem
        
        # Configure merge arguments (DuoSubs expects Path objects, not strings)
        args = MergeArgs(
            primary=Path(primary_host),
            secondary=Path(secondary_host),
            output_dir=Path(output_dir_host),
            output_name=basename,
        )
        
        print(f"Merging: {primary_host} + {secondary_host}", flush=True)
        
        # Run merge pipeline with print as progress callback
        run_merge_pipeline(args, print)
        
        # DuoSubs creates basename.zip containing basename_combined.ass
        # Extract the combined file
        zip_path = Path(output_dir_host) / f"{basename}.zip"
        
        if not zip_path.exists():
            return MergeResponse(
                success=False,
                error=f"DuoSubs did not create expected ZIP file: {zip_path}"
            )
        
        # Extract ZIP
        import zipfile
        with zipfile.ZipFile(zip_path, 'r') as zip_ref:
            zip_ref.extractall(output_dir_host)
        
        # Find the combined file
        combined_file = Path(output_dir_host) / f"{basename}_combined.ass"
        
        if not combined_file.exists():
            return MergeResponse(
                success=False,
                error=f"Combined ASS file not found after extraction: {combined_file}"
            )
        
        # Clean up ZIP file
        zip_path.unlink()
        
        print(f"✅ Merge completed: {combined_file}", flush=True)
        
        # Translate back to container path for response
        output_container = str(combined_file).replace(host_prefix, request.container_prefix, 1)
        
        return MergeResponse(
            success=True,
            output_path=output_container
        )
        
    except Exception as e:
        return MergeResponse(
            success=False,
            error=f"Merge failed: {str(e)}"
        )


if __name__ == "__main__":
    # Get configuration from environment
    host = os.getenv("DUOSUBS_HOST", "0.0.0.0")
    port = int(os.getenv("DUOSUBS_PORT", "8765"))
    
    print(f"🚀 Starting DuoSubs Service on {host}:{port}")
    print(f"📁 Host media path: {os.getenv('HOST_MEDIA_PATH', 'not set - pass via request')}")
    
    uvicorn.run(app, host=host, port=port)
