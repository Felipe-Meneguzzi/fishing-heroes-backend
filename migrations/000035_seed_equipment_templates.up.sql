-- Seed: equipamentos iniciais (roll_ranges: campo de Stats -> [min,max])
INSERT INTO equipment_templates (id, name, type, roll_ranges, rune_slots, max_durability) VALUES
('rod_starter',  'Vara de Bambu',    'rod',  '{"FishingPower":[2,5],"RodHeight":[1,3]}',         1, 100),
('reel_starter', 'Molinete Simples', 'reel', '{"ReelForce":[2,5],"BaitSpeed":[0,0.05]}',         1, 100),
('line_starter', 'Linha de Nylon',   'line', '{"LineTension":[3,6],"EscapeReduction":[0,0.05]}', 1, 80);
