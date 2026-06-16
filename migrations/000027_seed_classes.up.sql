-- Seed: classes do MVP (base_stats cobre todos os atributos de domain.Stats)
INSERT INTO class_templates (id, name, description, base_stats) VALUES
('bruiser', 'Brutamontes', 'Foco em Força de Puxada. Derrota peixes grandes rápido, mas desgasta o equipamento muito mais.',
 '{"FishingPower":30,"ReelForce":10,"LineTension":20,"RodHeight":5,"BaitSpeed":0.0,"DoubleCatchChance":0.0,"LuckChance":0.05,"LuckPower":0.5,"EscapeReduction":0.0}'),
('trapper', 'Estrategista', 'Aumenta a velocidade de atração e tem chance de Pesca Dupla. Ideal para farmar materiais.',
 '{"FishingPower":18,"ReelForce":12,"LineTension":18,"RodHeight":6,"BaitSpeed":0.15,"DoubleCatchChance":0.05,"LuckChance":0.10,"LuckPower":0.5,"EscapeReduction":0.05}');
