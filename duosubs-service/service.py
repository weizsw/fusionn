#!/usr/bin/env python3
"""
DuoSubs HTTP Service
Runs on host to leverage Metal GPU acceleration.
Called by fusionn container via HTTP.
"""

import os
import time
import zipfile
from pathlib import Path
from typing import Optional

import logfmt

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
from pydantic import BaseModel
import uvicorn

# Import DuoSubs Python API
from duosubs import MergeArgs, run_merge_pipeline

# The shared renderer owns the format, level and redaction policy; logs go to
# stderr so the process keeps stdout free for machine results.
LOG = logfmt.configure("duosubs", component="service")

app = FastAPI(title="DuoSubs Service", version="1.0.0")


def collect_progress(limit=200, byte_limit=8192):
    """Return (progress_lines, callback) for DuoSubs progress output.

    The callback is what DuoSubs calls with each progress line: lines are kept
    in a bounded list (by count and total bytes) instead of being printed raw,
    and only a bounded DEBUG preview ever reaches the log.
    """
    progress: list[str] = []
    total = 0

    def collect(line):
        nonlocal total
        text = str(line).rstrip()[:byte_limit]
        progress.append(text)
        total += len(text.encode("utf-8", "replace"))
        while progress and (len(progress) > limit or total > byte_limit):
            total -= len(progress.pop(0).encode("utf-8", "replace"))

    return progress, collect


@app.middleware("http")
async def log_requests(request: Request, call_next):
    """One request_completed summary per request; request IDs are echoed."""
    started = time.perf_counter()
    incoming = logfmt.request_id(request.headers.get("X-Request-ID"))
    request.state.request_id = incoming
    try:
        response = await call_next(request)
    except Exception:
        # Exactly one ERROR for the failure: returning the response here (and
        # skipping the normal summary) keeps both uvicorn and this middleware
        # from reporting the same traceback again.
        LOG.exception(
            "request_completed",
            "Request failed",
            request_id=incoming,
            method=request.method,
            route=request.url.path,
            status=500,
            duration_ms=round((time.perf_counter() - started) * 1000, 1),
        )
        response = JSONResponse(status_code=500, content={"detail": "Internal Server Error"})
        response.headers["X-Request-ID"] = incoming
        return response
    response.headers["X-Request-ID"] = incoming
    logfmt.request_summary(
        LOG,
        request.url.path,
        request.method,
        response.status_code,
        incoming,
        round((time.perf_counter() - started) * 1000, 1),
    )
    return response


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


def _elapsed(started: float) -> float:
    """Milliseconds since started, rounded like the other duration_ms fields."""
    return round((time.perf_counter() - started) * 1000, 1)


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
async def merge_subtitles(request: MergeRequest, http_request: Request = None):
    """
    Merge two subtitle files using duosubs Python API.
    Paths are translated from container paths to host paths.
    """
    request_id = getattr(getattr(http_request, "state", None), "request_id", None)
    started = time.perf_counter()
    log = LOG

    # Get host prefix from environment
    host_prefix = os.getenv("HOST_MEDIA_PATH", request.host_prefix)

    # Translate paths
    primary_host = translate_path(request.primary_path, request.container_prefix, host_prefix)
    secondary_host = translate_path(request.secondary_path, request.container_prefix, host_prefix)
    output_dir_host = translate_path(request.output_dir, request.container_prefix, host_prefix)

    log.info("merge_started", "Merge started", request_id=request_id)
    log.debug("merge_detail", "Merge paths",
              request_id=request_id, preview=f"{primary_host} + {secondary_host} -> {output_dir_host}")

    # Validate input files exist
    if not Path(primary_host).exists():
        log.warn("merge_failed", "Primary subtitle not found",
                 request_id=request_id, reason="primary_not_found", duration_ms=_elapsed(started))
        return MergeResponse(
            success=False,
            error=f"Primary subtitle not found: {primary_host}"
        )
    
    if not Path(secondary_host).exists():
        log.warn("merge_failed", "Secondary subtitle not found",
                 request_id=request_id, reason="secondary_not_found", duration_ms=_elapsed(started))
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
        
        # Progress output is collected in a bounded list, never forwarded raw.
        progress, collect = collect_progress()
        run_merge_pipeline(args, collect)
        if progress:
            log.debug("merge_progress", "Merge progress",
                      request_id=request_id, preview=" | ".join(progress[-5:]))
        
        # DuoSubs creates basename.zip containing basename_combined.ass
        # Extract the combined file
        zip_path = Path(output_dir_host) / f"{basename}.zip"
        
        if not zip_path.exists():
            log.warn("merge_failed", "DuoSubs did not create the expected ZIP file",
                     request_id=request_id, reason="missing_zip", duration_ms=_elapsed(started))
            return MergeResponse(
                success=False,
                error=f"DuoSubs did not create expected ZIP file: {zip_path}"
            )
        
        # Extract ZIP
        with zipfile.ZipFile(zip_path, 'r') as zip_ref:
            zip_ref.extractall(output_dir_host)
        
        # Find the combined file
        combined_file = Path(output_dir_host) / f"{basename}_combined.ass"
        
        if not combined_file.exists():
            log.warn("merge_failed", "Combined ASS file not found after extraction",
                     request_id=request_id, reason="missing_combined", duration_ms=_elapsed(started))
            return MergeResponse(
                success=False,
                error=f"Combined ASS file not found after extraction: {combined_file}"
            )
        
        # Clean up ZIP file
        zip_path.unlink()
        
        # Translate back to container path for response
        output_container = str(combined_file).replace(host_prefix, request.container_prefix, 1)

        log.info("merge_completed", "Merge completed",
                 request_id=request_id, duration_ms=_elapsed(started))
        log.debug("merge_output", "Merge output", request_id=request_id, preview=output_container)
        
        return MergeResponse(
            success=True,
            output_path=output_container
        )
        
    except Exception as error:
        # The exception text is content we did not author: the event keeps the
        # type, a bounded excerpt stays in a DEBUG preview and in the response.
        log.error("merge_failed", "Merge failed",
                  request_id=request_id, reason="merge_error", error=type(error).__name__,
                  duration_ms=_elapsed(started))
        log.debug("merge_error_detail", "Merge error detail",
                  request_id=request_id, preview=str(error))
        return MergeResponse(
            success=False,
            error=f"Merge failed: {str(error)}"
        )


if __name__ == "__main__":
    # Get configuration from environment
    host = os.getenv("DUOSUBS_HOST", "0.0.0.0")
    port = int(os.getenv("DUOSUBS_PORT", "8765"))
    configure_logging = logfmt.configure

    configure_logging("duosubs", component="service")
    LOG.info("service_started", "Service started",
             host=host, port=port,
             host_media_path=os.getenv("HOST_MEDIA_PATH", ""),
             log_level=logfmt.resolve_level()[0])

    # log_config=None: uvicorn must not install its own handlers, so its
    # access/error records reach the shared formatter instead of bypassing it.
    uvicorn.run(app, host=host, port=port, log_config=None)
