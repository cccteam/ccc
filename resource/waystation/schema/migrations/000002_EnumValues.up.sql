INSERT INTO WorkOrderStatuses (Id, Description) VALUES ('draft', 'Draft');
INSERT INTO WorkOrderStatuses (Id, Description) VALUES ('scheduled', 'Scheduled');
INSERT INTO WorkOrderStatuses (Id, Description) VALUES ('in_progress', 'In progress');
INSERT INTO WorkOrderStatuses (Id, Description) VALUES ('on_hold', 'On hold');
INSERT INTO WorkOrderStatuses (Id, Description) VALUES ('completed', 'Completed');
INSERT INTO WorkOrderStatuses (Id, Description) VALUES ('cancelled', 'Cancelled');

INSERT INTO RequisitionStatuses (Id, Description) VALUES ('draft', 'Draft');
INSERT INTO RequisitionStatuses (Id, Description) VALUES ('submitted', 'Submitted');
INSERT INTO RequisitionStatuses (Id, Description) VALUES ('approved', 'Approved');
INSERT INTO RequisitionStatuses (Id, Description) VALUES ('declined', 'Declined');
INSERT INTO RequisitionStatuses (Id, Description) VALUES ('fulfilled', 'Fulfilled');

INSERT INTO ItemCategories (Id, Description) VALUES ('consumable', 'Consumable');
INSERT INTO ItemCategories (Id, Description) VALUES ('spare_part', 'Spare part');
INSERT INTO ItemCategories (Id, Description) VALUES ('tool', 'Tool');
INSERT INTO ItemCategories (Id, Description) VALUES ('hazmat', 'Hazardous material');

INSERT INTO DeclineReasons (Id, Description) VALUES ('over_budget', 'Over budget');
INSERT INTO DeclineReasons (Id, Description) VALUES ('duplicate', 'Duplicate request');
INSERT INTO DeclineReasons (Id, Description) VALUES ('not_needed', 'Not needed');
INSERT INTO DeclineReasons (Id, Description) VALUES ('supplier_issue', 'Supplier issue');
