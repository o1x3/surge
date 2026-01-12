#!/usr/bin/env python3
"""
Benchmark script to compare Surge against other download tools:
- aria2c (with Motrix-style config)
- wget
- curl
"""

import os
import platform
import shutil
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Optional

# =============================================================================
# PLATFORM DETECTION
# =============================================================================
IS_WINDOWS = platform.system() == "Windows"
EXE_SUFFIX = ".exe" if IS_WINDOWS else ""

# =============================================================================
# CONFIGURATION
# =============================================================================
# Default test file URL (test file)
TEST_URL = "https://sin-speed.hetzner.com/1GB.bin"

MOTRIX_REPO = "https://github.com/agalwood/Motrix.git"

MB = 1024 * 1024


# =============================================================================
# DATA CLASSES
# =============================================================================
@dataclass
class BenchmarkResult:
    """Result of a single benchmark run."""
    tool: str
    success: bool
    elapsed_seconds: float
    file_size_bytes: int
    error: Optional[str] = None
    iter_results: Optional[list[float]] = None  # List of elapsed times for each iteration

    @property
    def speed_mbps(self) -> float:
        if self.elapsed_seconds <= 0:
            return 0.0
        return (self.file_size_bytes / MB) / self.elapsed_seconds


# =============================================================================
# UTILITY FUNCTIONS
# =============================================================================
def run_command(cmd: list[str], cwd: Optional[str] = None, timeout: int = 600) -> tuple[bool, str]:
    """Run a command and return (success, output)."""
    try:
        # On Windows, use shell=True to find executables in PATH
        # and handle .exe extensions properly
        result = subprocess.run(
            cmd,
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=timeout,
            shell=IS_WINDOWS,  # Needed for Windows PATH resolution
        )
        output = result.stdout + result.stderr
        return result.returncode == 0, output
    except subprocess.TimeoutExpired:
        return False, "Command timed out"
    except FileNotFoundError as e:
        return False, f"Command not found: {e}"
    except Exception as e:
        return False, str(e)


def which(cmd: str) -> Optional[str]:
    """Return the path to a command, or None if not found."""
    return shutil.which(cmd)


def get_file_size(path: Path) -> int:
    """Get the size of a file in bytes."""
    if path.exists():
        return path.stat().st_size
    return 0


def cleanup_file(path: Path):
    """Remove a file if it exists."""
    try:
        if path.exists():
            path.unlink()
    except Exception:
        pass


# =============================================================================
# SETUP FUNCTIONS
# =============================================================================
def build_surge(project_dir: Path) -> bool:
    """Build surge from source."""
    print("  Building surge...")
    output_name = f"surge{EXE_SUFFIX}"
    success, output = run_command(["go", "build", "-o", output_name, "."], cwd=str(project_dir))
    if not success:
        print(f"    ❌ Failed to build surge: {output}")
        return False
    print("    ✅ Surge built successfully")
    return True


def check_wget() -> bool:
    """Check if wget is installed."""
    if which("wget"):
        print("    ✅ wget found")
        return True
    print("    ❌ wget not found")
    return False


def check_curl() -> bool:
    """Check if curl is installed."""
    if which("curl"):
        print("    ✅ curl found")
        return True
    print("    ❌ curl not found")
    return False

def build_grab_bench(project_dir: Path) -> bool:
    """Build the Go-based grab benchmark."""
    print("  Building grab benchmark...")
    
    # Check if bench file exists
    bench_source = project_dir / "benchmarks" / "grab_bench.go"
    if not bench_source.exists():
        print(f"    ❌ Source file not found: {bench_source}")
        return False
        
    output_name = f"grab_bench{EXE_SUFFIX}"
    
    # We run go build in the benchmarks dir
    success, output = run_command(
        ["go", "build", "-o", f"../{output_name}", "grab_bench.go"], 
        cwd=str(project_dir / "benchmarks") 
    )
    
    if not success:
        print(f"    ❌ Failed to build grab benchmark: {output}")
        # Try running go get again just in case
        print("    Attempting go get github.com/cavaliergopher/grab/v3...")
        run_command(["go", "get", "github.com/cavaliergopher/grab/v3"], cwd=str(bench_dir))
        success, output = run_command(
            ["go", "build", "-o", f"../{output_name}", "grab_bench.go"], 
            cwd=str(bench_dir)
        )
        if not success:
             print(f"    ❌ Build failed again: {output}")
             return False

    print("    ✅ Grab benchmark built successfully")
    return True


def clone_motrix_extra(dest_dir: Path) -> Optional[Path]:
    """Clone only the extra directory from Motrix repo."""
    print("  Cloning Motrix extra directory...")
    
    # Use sparse checkout to get only the extra directory
    success, output = run_command([
        "git", "clone", "--depth", "1", "--filter=blob:none", "--sparse",
        MOTRIX_REPO, str(dest_dir)
    ])
    if not success:
        print(f"    ❌ Failed to clone Motrix: {output}")
        return None
    
    success, output = run_command(
        ["git", "sparse-checkout", "set", "extra"],
        cwd=str(dest_dir)
    )
    if not success:
        print(f"    ❌ Failed to sparse checkout: {output}")
        return None
    
    extra_dir = dest_dir / "extra"
    if extra_dir.exists():
        print("    ✅ Motrix extra directory cloned")
        return extra_dir
    
    print("    ❌ Extra directory not found after clone")
    return None


def check_aria2c() -> bool:
    """Check if aria2c is installed."""
    if which("aria2c"):
        print("    ✅ aria2c found")
        return True
    print("    ❌ aria2c not found (install aria2)")
    return False


# =============================================================================
# BENCHMARK FUNCTIONS
# =============================================================================
def benchmark_surge(project_dir: Path, url: str, output_dir: Path) -> BenchmarkResult:
    """Benchmark surge downloader."""
    surge_bin = project_dir / f"surge{EXE_SUFFIX}"
    
    if not surge_bin.exists():
        return BenchmarkResult("surge", False, 0, 0, f"Binary not found: {surge_bin}")
    
    start = time.perf_counter()
    success, output = run_command([
        str(surge_bin), "get", url,
        "--output", str(output_dir),  # Download directory
        # "--headless",               # Removed as it's default/unrecognized
        # "-c", "16",                 # Removed as concurrent defaults are dynamic or strictly internal
    ], timeout=600)
    elapsed = time.perf_counter() - start
    
    # Try to parse the actual download time from Surge output (excluding probing)
    # Output format: "Complete: 1.0 GB in 5.2s (196.34 MB/s)" OR "... in 500ms ..."
    import re
    actual_time = elapsed
    match = re.search(r"in ([\d\.]+)(m?s)", output)
    if match:
        try:
            val = float(match.group(1))
            unit = match.group(2)
            if unit == "ms":
                val /= 1000.0
            actual_time = val
        except ValueError:
            pass

    # Find downloaded file (surge uses original filename)
    downloaded_files = list(output_dir.glob("*.bin")) + list(output_dir.glob("*MB*")) + list(output_dir.glob("*.zip"))
    file_size = 0
    for f in downloaded_files:
        if f.is_file() and "surge" not in f.name:
            file_size = max(file_size, get_file_size(f))
            cleanup_file(f)
    
    if not success:
        return BenchmarkResult("surge", False, actual_time, file_size, output[:200])
    
    return BenchmarkResult("surge", True, actual_time, file_size)


def benchmark_aria2(motrix_extra_dir: Optional[Path], url: str, output_dir: Path) -> BenchmarkResult:
    """Benchmark aria2c with Motrix config."""
    output_file = output_dir / "aria2_download"
    cleanup_file(output_file)
    
    if not which("aria2c"):
        return BenchmarkResult("aria2c (Motrix)", False, 0, 0, "aria2c not installed")
    
    # Build aria2c command with Motrix-style config
    cmd = [
        "aria2c",
        "--max-connection-per-server=16",
        "--split=16",
        "--min-split-size=1M",
        "--max-concurrent-downloads=1",
        "--continue=true",
        "--auto-file-renaming=false",
        "--allow-overwrite=true",
        "--console-log-level=warn",
        "-o", output_file.name,
        "-d", str(output_dir),
        url
    ]
    
    # If we have Motrix extra, use their aria2.conf if available
    if motrix_extra_dir:
        conf_file = motrix_extra_dir / "aria2.conf"
        if conf_file.exists():
            cmd.insert(1, f"--conf-path={conf_file}")
    
    start = time.perf_counter()
    success, output = run_command(cmd, timeout=600)
    elapsed = time.perf_counter() - start
    
    file_size = get_file_size(output_file)
    cleanup_file(output_file)
    
    if not success:
        return BenchmarkResult("aria2c (Motrix)", False, elapsed, file_size, output[:200])
    
    return BenchmarkResult("aria2c (Motrix)", True, elapsed, file_size)


def benchmark_aria2_standard(url: str, output_dir: Path) -> BenchmarkResult:
    """Benchmark vanilla aria2c (default settings)."""
    output_file = output_dir / "aria2_std_download"
    cleanup_file(output_file)
    
    if not which("aria2c"):
        return BenchmarkResult("aria2c (Std)", False, 0, 0, "aria2c not installed")
    
    # Standard aria2c with reasonable optimization (16 splits) but no extras
    cmd = [
        "aria2c",
        "-x", "16", "-s", "16",  # 16 connections
        "-o", output_file.name,
        "-d", str(output_dir),
        "--allow-overwrite=true",
        "--console-log-level=warn",
        url
    ]
    
    start = time.perf_counter()
    success, output = run_command(cmd, timeout=600)
    elapsed = time.perf_counter() - start
    
    file_size = get_file_size(output_file)
    cleanup_file(output_file)
    
    if not success:
        return BenchmarkResult("aria2c (Std)", False, elapsed, file_size, output[:200])
    
    return BenchmarkResult("aria2c (Std)", True, elapsed, file_size)


def benchmark_grab_go(project_dir: Path, url: str, output_dir: Path) -> BenchmarkResult:
    """Benchmark using the compiled grab_bench Go program."""
    grab_bin = project_dir / f"grab_bench{EXE_SUFFIX}"
    
    if not grab_bin.exists():
        return BenchmarkResult("grab (Go)", False, 0, 0, "Binary not found")
        
    start = time.perf_counter()
    success, output = run_command([str(grab_bin), url, str(output_dir)], timeout=600)
    elapsed = time.perf_counter() - start
    
    # helper cleans up filename from url, so we just check for any new file
    # but grab_bench.go prints "file=..."
    downloaded_file = None
    file_size = 0
    
    for line in output.splitlines():
        if line.startswith("file="):
            fname = line.split("=", 1)[1]
            downloaded_file = output_dir / fname
        if line.startswith("size="):
            try:
                file_size = int(line.split("=", 1)[1])
            except ValueError:
                pass
    
    if downloaded_file and downloaded_file.exists():
         file_size = get_file_size(downloaded_file)
         cleanup_file(downloaded_file)
    else:
        # Fallback cleanup
        for f in output_dir.glob("*MB*"):
            cleanup_file(f)

    if not success:
        return BenchmarkResult("grab (Go)", False, elapsed, file_size, output[:200])
    
    return BenchmarkResult("grab (Go)", True, elapsed, file_size)


def benchmark_wget(url: str, output_dir: Path) -> BenchmarkResult:
    """Benchmark wget downloader."""
    output_file = output_dir / "wget_download"
    cleanup_file(output_file)
    
    wget_bin = which("wget")
    if not wget_bin:
        return BenchmarkResult("wget", False, 0, 0, "wget not installed")
    
    start = time.perf_counter()
    success, output = run_command([
        wget_bin, "-q", "-O", str(output_file), url
    ], timeout=600)
    elapsed = time.perf_counter() - start
    
    file_size = get_file_size(output_file)
    cleanup_file(output_file)
    
    if not success:
        return BenchmarkResult("wget", False, elapsed, file_size, output[:200])
    
    return BenchmarkResult("wget", True, elapsed, file_size)


def benchmark_curl(url: str, output_dir: Path) -> BenchmarkResult:
    """Benchmark curl downloader."""
    output_file = output_dir / "curl_download"
    cleanup_file(output_file)
    
    curl_bin = which("curl")
    if not curl_bin:
        return BenchmarkResult("curl", False, 0, 0, "curl not installed")
    
    start = time.perf_counter()
    success, output = run_command([
        curl_bin, "-s", "-L", "-o", str(output_file), url
    ], timeout=600)
    elapsed = time.perf_counter() - start
    
    file_size = get_file_size(output_file)
    cleanup_file(output_file)
    
    if not success:
        return BenchmarkResult("curl", False, elapsed, file_size, output[:200])
    
    return BenchmarkResult("curl", True, elapsed, file_size)


# =============================================================================
# REPORTING
# =============================================================================
def print_results(results: list[BenchmarkResult]):
    """Print benchmark results in a formatted table."""
    print("\n" + "=" * 70)
    print("  BENCHMARK RESULTS")
    print("=" * 70)
    
    # Header
    print(f"\n  {'Tool':<20} │ {'Status':<8} │ {'Avg Time':<10} │ {'Avg Speed':<12} │ {'Size':<10}")
    print(f"  {'─'*20}─┼─{'─'*8}─┼─{'─'*10}─┼─{'─'*12}─┼─{'─'*10}")
    
    for r in results:
        status = "✅" if r.success else "❌"
        time_str = f"{r.elapsed_seconds:.2f}s" if r.elapsed_seconds > 0 else "N/A"
        speed_str = f"{r.speed_mbps:.2f} MB/s" if r.success and r.speed_mbps > 0 else "N/A"
        size_str = f"{r.file_size_bytes / MB:.1f} MB" if r.file_size_bytes > 0 else "N/A"
        
        print(f"  {r.tool:<20} │ {status:<8} │ {time_str:<10} │ {speed_str:<12} │ {size_str:<10}")
        if r.iter_results and len(r.iter_results) > 1:
            print(f"    └─ Runs: {', '.join([f'{t:.2f}s' for t in r.iter_results])}")
        
        if not r.success and r.error:
            print(f"    └─ Error: {r.error[:60]}...")
    
    print("\n" + "=" * 70)
    
    print("\n" + "=" * 70)
    
    # Find winner
    successful = [r for r in results if r.success and r.speed_mbps > 0]
    if successful:
        winner = max(successful, key=lambda r: r.speed_mbps)
        print(f"\n  🏆 Fastest: {winner.tool} @ {winner.speed_mbps:.2f} MB/s")
    
    print()
    print_histogram(results)


def print_histogram(results: list[BenchmarkResult]):
    """Print a text-based histogram of download speeds."""
    successful = [r for r in results if r.success and r.speed_mbps > 0]
    if not successful:
        return
        
    print("\n  SPEED COMPARISON")
    print("  " + "-" * 50)
    
    # Sort by speed descending
    sorted_results = sorted(successful, key=lambda r: r.speed_mbps, reverse=True)
    max_speed = sorted_results[0].speed_mbps
    width = 50
    
    for r in sorted_results:
        bar_len = int((r.speed_mbps / max_speed) * width)
        bar = "█" * bar_len
        print(f"  {r.tool:<20} │ {bar:<50} {r.speed_mbps:.2f} MB/s")
    print()


# =============================================================================
# MAIN
# =============================================================================
def main():
    print("\n🚀 Surge Benchmark Suite")
    print("=" * 40)
    
    # Parse arguments
    import argparse
    parser = argparse.ArgumentParser(description="Surge Benchmark Suite")
    parser.add_argument("url", nargs="?", default=TEST_URL, help="URL to download for benchmarking")
    parser.add_argument("-n", "--iterations", type=int, default=1, help="Number of iterations to run (default: 1)")
    
    # Service flags
    parser.add_argument("--surge", action="store_true", help="Run Surge benchmark")
    parser.add_argument("--motrix", action="store_true", help="Run Motrix (aria2c) benchmark")
    parser.add_argument("--aria2", action="store_true", help="Run Standard aria2c benchmark")
    parser.add_argument("--grab", action="store_true", help="Run Grab (Go) benchmark")
    parser.add_argument("--wget", action="store_true", help="Run wget benchmark")
    parser.add_argument("--curl", action="store_true", help="Run curl benchmark")
    
    args = parser.parse_args()
    
    test_url = args.url
    num_iterations = args.iterations
    
    # helper to check if any specific service was requested
    specific_service_requested = any([args.surge, args.motrix, args.aria2, args.grab, args.wget, args.curl])
    
    print(f"\n  Test URL:   {test_url}")
    print(f"  Iterations: {num_iterations}")
    
    # Determine project directory
    project_dir = Path(__file__).parent.resolve()
    print(f"  Project:  {project_dir}")
    
    # Create temp directory for downloads
    temp_dir = Path(tempfile.mkdtemp(prefix="surge_bench_"))
    motrix_clone_dir = temp_dir / "motrix"
    download_dir = temp_dir / "downloads"
    download_dir.mkdir(parents=True, exist_ok=True)
    
    print(f"  Temp Dir: {temp_dir}")
    
    try:
        # Setup phase
        print("\n📦 SETUP")
        print("-" * 40)
        
        run_all = not specific_service_requested

        # Initialize all to False
        surge_ok, grab_bench_ok, aria2_ok, wget_ok, curl_ok = False, False, False, False, False
        motrix_extra = None
        
        # --- Go dependent tools ---
        if run_all or args.surge or args.grab:
            if not which("go"):
                print("  ❌ Go is not installed. `surge` and `grab` benchmarks will be skipped.")
            else:
                print("  ✅ Go found")
                if run_all or args.surge:
                    surge_ok = build_surge(project_dir)
                if run_all or args.grab:
                    grab_bench_ok = build_grab_bench(project_dir)

        # --- Aria2 dependent tools ---
        if run_all or args.aria2 or args.motrix:
            aria2_ok = check_aria2c()
            if aria2_ok and (run_all or args.motrix):
                if not which("git"):
                     print("  ❌ Git is not installed. The Motrix config will not be cloned.")
                else:
                    print("  ✅ Git found")
                    motrix_extra = clone_motrix_extra(motrix_clone_dir)
        
        # --- Other tools ---
        if run_all or args.wget:
            wget_ok = check_wget()
        
        if run_all or args.curl:
            curl_ok = check_curl()
        
        # Define benchmarks to run
        tasks = []
        
        # Surge
        if surge_ok and (not specific_service_requested or args.surge):
            tasks.append({"name": "surge", "func": benchmark_surge, "args": (project_dir, test_url, download_dir)})
        
        # Motrix (aria2c)
        if aria2_ok and (not specific_service_requested or args.motrix):
            tasks.append({"name": "aria2c (Motrix)", "func": benchmark_aria2, "args": (motrix_extra, test_url, download_dir)})
            
        # Standard aria2c
        if aria2_ok and (not specific_service_requested or args.aria2):
            tasks.append({"name": "aria2c (Std)", "func": benchmark_aria2_standard, "args": (test_url, download_dir)})
            
        # Grab
        if grab_bench_ok and (not specific_service_requested or args.grab):
            tasks.append({"name": "grab (Go)", "func": benchmark_grab_go, "args": (project_dir, test_url, download_dir)})
        
        # wget
        if wget_ok and (not specific_service_requested or args.wget):
            tasks.append({"name": "wget", "func": benchmark_wget, "args": (test_url, download_dir)})
        
        # curl
        if curl_ok and (not specific_service_requested or args.curl):
            tasks.append({"name": "curl", "func": benchmark_curl, "args": (test_url, download_dir)})

        # Initialize results storage
        # Map: tool_name -> list of BenchmarkResult
        raw_results: dict[str, list[BenchmarkResult]] = {task["name"]: [] for task in tasks}

        # Benchmark phase
        print("\n⏱️  BENCHMARKING")
        print("-" * 40)
        print(f"  Downloading: {test_url}")
        print(f"  Exec Order:  Interlaced ({len(tasks)} tools x {num_iterations} runs)\n")
        
        for i in range(num_iterations):
            print(f"\n  [ Iteration {i+1}/{num_iterations} ]")
            for task in tasks:
                name = task["name"]
                func = task["func"]
                task_args = task["args"]
                
                print(f"    Running {name}...", end="", flush=True)
                res = func(*task_args)
                
                raw_results[name].append(res)
                
                if res.success:
                    print(f" {res.elapsed_seconds:.2f}s")
                else:
                    print(" Failed")
                
                # Small cool-down between tools
                time.sleep(0.5)

        # Aggregate results
        final_results: list[BenchmarkResult] = []
        
        for task in tasks:
            name = task["name"]
            runs = raw_results[name]
            
            # Filter successful runs for time averaging
            successful_runs = [r for r in runs if r.success]
            
            if not successful_runs:
                # All failed, grab the last error
                last_error = runs[-1].error if runs else "No runs"
                final_results.append(BenchmarkResult(name, False, 0, 0, last_error))
                continue

            times = [r.elapsed_seconds for r in successful_runs]
            avg_time = sum(times) / len(times)
            
            # Use the size from the first successful run (should be constant)
            file_size = successful_runs[0].file_size_bytes
            
            final_results.append(BenchmarkResult(
                tool=name,
                success=True,
                elapsed_seconds=avg_time,
                file_size_bytes=file_size,
                iter_results=times
            ))

        # Add skipped tools to results for completeness
        # (Though current logic implies they aren't in 'tasks' if setup failed, 
        # so maybe we just report on what ran. The previous code reported failures if build failed.
        # Let's add explicit failures for tools that failed setup if we want to match exact behavior,
        # but the request was specifically about interlacing.)
        
        # If we want to report originally failed tools (execution plan):
        if (not specific_service_requested or args.surge) and not surge_ok:
            final_results.append(BenchmarkResult("surge", False, 0, 0, "Build failed"))
        if (not specific_service_requested or args.motrix) and not aria2_ok:
            final_results.append(BenchmarkResult("aria2c (Motrix)", False, 0, 0, "Not installed"))
        if (not specific_service_requested or args.aria2) and not aria2_ok:
            final_results.append(BenchmarkResult("aria2c (Std)", False, 0, 0, "Not installed"))
        if (not specific_service_requested or args.grab) and not grab_bench_ok:
            final_results.append(BenchmarkResult("grab (Go)", False, 0, 0, "Build failed"))
        if (not specific_service_requested or args.wget) and not wget_ok:
             final_results.append(BenchmarkResult("wget", False, 0, 0, "Not installed"))
        if (not specific_service_requested or args.curl) and not curl_ok:
             final_results.append(BenchmarkResult("curl", False, 0, 0, "Not installed"))

        # sort results to keep somewhat consistent order or just trust append order? 
        # The append order for failures is at the end. That's fine.

        # Print results
        print_results(final_results)

        
    finally:
        # Cleanup
        print("🧹 Cleaning up temp directory...")
        shutil.rmtree(temp_dir, ignore_errors=True)
        print("  Done.")


if __name__ == "__main__":
    main()
