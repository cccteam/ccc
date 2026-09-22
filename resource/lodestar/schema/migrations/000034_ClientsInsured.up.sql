-- Insured records whether the outfit carries salvage cover: unknown until the booking
-- desk hears from the underwriter, then yes or no. The column is nullable on purpose:
-- the third state is the story, and it is what makes the generator type the field
-- NullBoolean for the browser (the demo's one nullable BOOL).
ALTER TABLE Clients ADD COLUMN Insured BOOL;
