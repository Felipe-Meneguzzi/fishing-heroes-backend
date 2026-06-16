-- Seed: peixes do Mundo 1
-- vendor: vira troféu se o tamanho sorteado >= 80% do máximo; senão auto-venda.
INSERT INTO fish_templates
  (id, name, category, rarity, min_weight, max_weight, stamina, force, gold_value, xp, material_id, rune_template_id, species_id) VALUES
('lambari',      'Lambari',      'vendor',   0, 0.05, 0.30, 12,  8,  3,  5, NULL, NULL, 'lambari'),
('traira',       'Traíra',       'vendor',   1, 0.50, 3.00, 40, 30, 15, 20, NULL, NULL, 'traira'),
('dourado',      'Dourado',      'vendor',   2, 2.00, 12.00,80, 60, 60, 60, NULL, NULL, 'dourado'),
('cascudo',      'Cascudo',      'material', 1, 0,    0,    25, 20,  0, 12, 'escama_dura',    NULL, NULL),
('bagre',        'Bagre',        'material', 2, 0,    0,    55, 45,  0, 30, 'espinho_afiado', NULL, NULL),
('peixe_runico', 'Peixe Rúnico', 'rune',     3, 0,    0,    60, 50,  0, 80, NULL, 'rune_forca', NULL);
