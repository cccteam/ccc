-- The hazard scale is 1..5. The create validator (MissionCreateValidator) answers the rule
-- as 400 naming the field before anything is buffered; the update path has no validator,
-- so this constraint is what refuses an update to hazard 9, at commit, and the library
-- answers the refusal as 400 in the resource's name. It is the demo's only CHECK a request
-- can reach: the others guard server-minted ids.
ALTER TABLE Missions ADD CONSTRAINT CK_Missions_Hazard CHECK (Hazard BETWEEN 1 AND 5);
