#!/usr/bin/env python3
"""
retry_enrich_tasks.py — Force-retry all DEADLINE_EXCEEDED tasks in the enrich-products queue.

Cloud Tasks backs off aggressively after repeated 504s, so tasks may not be
retried for hours. This script forces an immediate retry on all pending tasks
for a specific job, which will use the newly-deployed import-enrich with
shorter backoffs.

Usage:
    python3 retry_enrich_tasks.py --list               # show pending tasks
    python3 retry_enrich_tasks.py --run JOB_ID         # force-retry tasks for a job
    python3 retry_enrich_tasks.py --run-all            # force-retry ALL pending tasks
"""
import argparse
import subprocess
import sys
import time

PROJECT  = "marketmate-486116"
QUEUE    = "enrich-products"
LOCATION = "europe-west2"

parser = argparse.ArgumentParser()
parser.add_argument("--list",    action="store_true")
parser.add_argument("--run",     default=None, metavar="JOB_ID")
parser.add_argument("--run-all", action="store_true")
args = parser.parse_args()

def gcloud(cmd):
    r = subprocess.run(cmd, shell=True, capture_output=True, text=True)
    return r.returncode, r.stdout, r.stderr

def list_tasks(job_id=None):
    rc, out, err = gcloud(
        f'gcloud tasks list --queue={QUEUE} --location={LOCATION} '
        f'--project={PROJECT} --limit=500 '
        f'--format="value(name,scheduleTime,dispatchCount,lastAttemptStatus)"'
    )
    tasks = []
    for line in out.strip().splitlines():
        parts = line.split()
        if not parts: continue
        name = parts[0].split("/")[-1]  # just the task name
        if job_id and job_id not in name:
            continue
        tasks.append(name)
    return tasks

def force_retry(task_name):
    rc, out, err = gcloud(
        f'gcloud tasks run "{task_name}" --queue={QUEUE} '
        f'--location={LOCATION} --project={PROJECT}'
    )
    return rc == 0

if args.list:
    tasks = list_tasks()
    print(f"\nPending tasks in {QUEUE}: {len(tasks)}")
    for t in tasks[:20]:
        print(f"  {t}")
    if len(tasks) > 20:
        print(f"  ... and {len(tasks)-20} more")

elif args.run or args.run_all:
    job_id = args.run if args.run else None
    tasks = list_tasks(job_id)
    print(f"\nForce-retrying {len(tasks)} tasks{' for job '+job_id if job_id else ''}...")
    ok = 0
    fail = 0
    for i, task in enumerate(tasks):
        if force_retry(task):
            ok += 1
        else:
            fail += 1
        if (i+1) % 10 == 0:
            print(f"  {i+1}/{len(tasks)} done ({ok} ok, {fail} failed)...")
            time.sleep(0.5)  # gentle rate limiting on gcloud calls
    print(f"\nDone: {ok} retried, {fail} failed")
else:
    parser.print_help()
