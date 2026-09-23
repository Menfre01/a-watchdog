-- 0002_add_provider_probe_model.sql —— providers 增加「测试模型」:连通性测试所用模型名。

ALTER TABLE providers ADD COLUMN probe_model TEXT NOT NULL DEFAULT '';
