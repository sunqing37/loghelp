"""Summarize manually collected memory profiles; does not launch or test apps."""
import argparse
import csv
import json
import statistics
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument('directories', nargs='+', type=Path)
parser.add_argument('--since', type=float, default=60)
args = parser.parse_args()
summaries = []
for directory in args.directories:
    with (directory / 'process.csv').open(encoding='utf-8-sig', newline='') as stream:
        process = [{key: float(value) for key, value in row.items()} for row in csv.DictReader(stream)]
    memory = [json.loads(line) for line in (directory / 'memory.jsonl').read_text(encoding='utf-8-sig').splitlines() if line.strip()]
    process = [row for row in process if row['Seconds'] >= args.since]
    memory = [row for row in memory if row['elapsed_seconds'] >= args.since and row['goroutines'] > 2]
    if not process or len(memory) < 2:
        print(f'{directory}: waiting for samples after {args.since:g}s')
        continue
    first, last = memory[0], memory[-1]
    summary = {
        'directory': str(directory),
        'sample_interval_seconds': [process[0]['Seconds'], process[-1]['Seconds']],
        'working_set_mib': {
            'min': min(row['WorkingSet'] for row in process) / 2**20,
            'mean': statistics.mean(row['WorkingSet'] for row in process) / 2**20,
            'max': max(row['WorkingSet'] for row in process) / 2**20,
            'last': process[-1]['WorkingSet'] / 2**20,
        },
        'private_bytes_mib': {
            'mean': statistics.mean(row['PrivateBytes'] for row in process) / 2**20,
            'max': max(row['PrivateBytes'] for row in process) / 2**20,
            'last': process[-1]['PrivateBytes'] / 2**20,
        },
        'heap_alloc_mib': {
            'min': min(row['heap_alloc'] for row in memory) / 2**20,
            'max': max(row['heap_alloc'] for row in memory) / 2**20,
        },
        'allocation_mib_per_second': (last['total_alloc'] - first['total_alloc']) / (last['elapsed_seconds'] - first['elapsed_seconds']) / 2**20,
        'gpu_images_mib': sorted({round(row.get('gpu_image_bytes', 0) / 2**20, 2) for row in memory}),
        'graphics_library': last.get('graphics_library'),
        'forced_gc': last.get('forced_gc'),
        'goroutines_last_active_sample': last['goroutines'],
        'cpu_core_seconds_per_second': (process[-1]['CPU'] - process[0]['CPU']) / (process[-1]['Seconds'] - process[0]['Seconds']) if len(process) > 1 else None,
    }
    summaries.append(summary)
    print(json.dumps(summary, ensure_ascii=False, indent=2))
if summaries:
    (args.directories[-1].parent / 'memory-summary.json').write_text(json.dumps(summaries, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
