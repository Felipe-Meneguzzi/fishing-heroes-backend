-- Seed: Skill Tree em cruz (núcleo genérico + 4 braços especializados)
INSERT INTO skill_node_templates (id, name, branch, requires, max_rank, bonus_per_rank, generic) VALUES
('skl_core',    'Núcleo do Pescador', 'core',    NULL,        5, '{"FishingPower":2,"LineTension":1}',       true),
('skl_power',   'Força Bruta',        'power',   'skl_core', 10, '{"FishingPower":4}',                       false),
('skl_luck',    'Presságio',          'luck',    'skl_core', 10, '{"LuckChance":0.02,"LuckPower":0.05}',     false),
('skl_speed',   'Atração Veloz',      'speed',   'skl_core', 10, '{"BaitSpeed":0.05}',                       false),
('skl_tension', 'Linha Tensa',        'tension', 'skl_core', 10, '{"LineTension":3,"EscapeReduction":0.03}', false);
