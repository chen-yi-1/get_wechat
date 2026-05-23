from funasr.models.fun_asr_nano.model import FunASRNano
from funasr import AutoModel

model_dir = r"FunAudioLLM\Fun-ASR-Nano-2512"
wav_path = r"FunAudioLLM\Fun-ASR-Nano-2512\example\zh.mp3"

model = AutoModel(
    model=model_dir,
    trust_remote_code=True,
    disable_update=True,
    # vad_model="fsmn-vad",
    vad_kwargs={"max_single_segment_time": 30000},
    device="cpu",
)
res = model.generate(input=[wav_path], cache={}, batch_size_s=0)
text = res[0]["text"]
print(text)