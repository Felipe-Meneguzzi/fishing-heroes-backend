-- Seed: iscas iniciais (1 consumível + 1 reutilizável/durável)
INSERT INTO bait_templates (id, name, kind, tier, bonus_stats, charges, durability) VALUES
('bait_minhoca', 'Minhoca',          'consumable', 0, '{"BaitSpeed":0.10}', 500, NULL),
('bait_colher',  'Colher Giratória', 'durable',    0, '{"LuckChance":0.05}', NULL, 200);
