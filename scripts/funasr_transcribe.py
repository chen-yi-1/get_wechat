#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Offline ASR via FunASR (https://github.com/modelscope/FunASR).
Daemon mode: 模型常驻内存，通过 socket 接收音频文件路径进行转写
Usage:
  funasr_transcribe.py --daemon <port>  # 启动守护进程
Env:
  FUNASR_MODEL_DIR - 模型目录路径，默认 FunAudioLLM/Fun-ASR-Nano-2512
  FUNASR_DEVICE   - 设备类型，默认 cpu
"""
from __future__ import annotations
from funasr.models.fun_asr_nano.model import FunASRNano

import argparse
import json
import os
import socket
import sys
from concurrent.futures import ThreadPoolExecutor
from funasr import AutoModel

_model = None
_model_dir = "../FunAudioLLM/Fun-ASR-Nano-2512"
_device = "cpu"


def transcribe(audio_path: str) -> dict:
    global _model
    if _model is None:
        return {"error": "FunASR model not loaded, please start daemon first"}
    res = _model.generate(input=[audio_path], cache={}, batch_size_s=0)
    if not res or "text" not in res[0]:
        return {"error": f"unexpected fun-asr-nano result: {res!r}"}
    return {"text": str(res[0]["text"]), "model": _model_dir}


def handle_client(conn: socket.socket, addr: tuple) -> None:
    try:
        data = conn.recv(4096)
        if not data:
            conn.close()
            return
        audio_path = data.decode("utf-8").strip()
        if not os.path.isfile(audio_path):
            result = json.dumps({"error": f"audio file not found: {audio_path}"}, ensure_ascii=False)
        else:
            try:
                result = json.dumps(transcribe(audio_path), ensure_ascii=False)
            except Exception as e:
                result = json.dumps({"error": str(e)}, ensure_ascii=False)
        conn.sendall(result.encode("utf-8"))
    except Exception as e:
        try:
            conn.sendall(json.dumps({"error": str(e)}, ensure_ascii=False).encode("utf-8"))
        except Exception:
            pass
    finally:
        conn.close()


def daemon_mode(port: int) -> None:
    global _model, _model_dir, _device
    _model_dir = os.environ.get("FUNASR_MODEL_DIR", _model_dir)
    _device = os.environ.get("FUNASR_DEVICE", _device).strip() or _device

    # 预加载模型
    if _model is None:
        print("Loading FunASR model...", flush=True)
        _model = AutoModel(
            model=_model_dir,
            trust_remote_code=True,
            disable_update=True,
            vad_kwargs={"max_single_segment_time": 30000},
            device=_device,
        )
        print("FunASR model loaded successfully", flush=True)

    print(f"FunASR daemon started, listening on port {port}", flush=True)

    server_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    server_sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server_sock.bind(("127.0.0.1", port))
    server_sock.listen(5)

    executor = ThreadPoolExecutor(max_workers=4)
    try:
        while True:
            conn, addr = server_sock.accept()
            executor.submit(handle_client, conn, addr)
    except KeyboardInterrupt:
        pass
    finally:
        executor.shutdown(wait=True)
        server_sock.close()


def main() -> None:
    if len(sys.argv) >= 2 and sys.argv[1] == "--daemon":
        parser = argparse.ArgumentParser(description="FunASR Daemon Mode")
        parser.add_argument("port", type=int, help="Port to listen on")
        args = parser.parse_args(sys.argv[2:])
        daemon_mode(args.port)
        return

    print("Error: Only daemon mode is supported", file=sys.stderr)
    print("Usage: funasr_transcribe.py --daemon <port>", file=sys.stderr)
    sys.exit(1)


if __name__ == "__main__":
    main()