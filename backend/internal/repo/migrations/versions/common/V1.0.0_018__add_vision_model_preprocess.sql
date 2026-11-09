-- 模型预处理规格(JSON:normalize/colorFormat 等),随模型下发给 worker 运行时。
-- YOLOX 等模型要求 normalize=false + BGR,缺省预处理会导致零检出。
ALTER TABLE sys_vision_model
    ADD COLUMN IF NOT EXISTS preprocess TEXT NOT NULL DEFAULT '{}';
