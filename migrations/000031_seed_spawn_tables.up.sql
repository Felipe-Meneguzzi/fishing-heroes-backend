-- Seed: tabelas de spawn
INSERT INTO spawn_tables (spawn_table_id, fish_template_id, weight) VALUES
-- 1-1 Campos: peixes pequenos/comuns
('spawn_campos', 'lambari',      60),
('spawn_campos', 'cascudo',      25),
('spawn_campos', 'traira',       12),
('spawn_campos', 'peixe_runico',  3),
-- 1-2 Montanhas: peixes maiores e mais raros
('spawn_montanhas', 'traira',       35),
('spawn_montanhas', 'dourado',      18),
('spawn_montanhas', 'bagre',        28),
('spawn_montanhas', 'peixe_runico',  9),
('spawn_montanhas', 'lambari',      10);
