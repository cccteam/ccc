# scanned rows / elapsed per shape and capture, 2026-09-11
| shape | seed: scanned / elapsed | medium: scanned / elapsed | large: scanned / elapsed | large-indexed: scanned / elapsed |
|---|---|---|---|---|
| `s1_missions_bare_tenancy_list` | 62 / 14.38  ms | 5062 / 47.4  ms | 20060 / 67.85  ms | 52 / 16.59  ms |
| `s1b_missions_filtered_list` | 62 / 11.87  ms | 5062 / 20.32  ms | 20060 / 89.11  ms | 1240 / 88.79  ms |
| `s1c_consignments_cursor_page` | 24 / 15.54  ms | 24 / 13.41  ms | 14 / 12.7  ms | 14 / 51.08  ms |
| `s2_ships_joinpath_list` | 22 / 12.97  ms | 4422 / 29.56  ms | 20822 / 108.27  ms | 20822 / 95.85  ms |
| `s3_squadron_memberships_twohop_list` | 22 / 18.03  ms | 6214 / 34.72  ms | 82 / 23.22  ms | 82 / 20.06  ms |
| `s3b_sorties_via_mission_list` | 24 / 19.77  ms | 11024 / 58.55  ms | 104024 / 1.27 secs | 104024 / 847.24  ms |
| `s4_missions_subject_set_list` | 83 / 49.14  ms | 5767 / 176.79  ms | 20138 / 220.18  ms | 20138 / 163.12  ms |
| `s4b_missions_subject_set_capabilities` | 104 / 96.55  ms | 5845 / 93.38  ms | 512886 / 1.72 secs | 20216 / 168.86  ms |
| `s5_missions_subject_value_list` | 69 / 50.92  ms | 5444 / 38.27  ms | 20088 / 82.03  ms | 20088 / 113.84  ms |
| `s6_missions_visible_projection_sort` | 62 / 14.99  ms | 5062 / 34.45  ms | 20060 / 72.86  ms | 20060 / 55.19  ms |
| `s6b_missions_masked_fee_sort` | 62 / 114.36  ms | 5062 / 29.12  ms | 20060 / 61.47  ms | 20060 / 61.73  ms |
| `s7_write_check_missions` | 1 / 18.67  ms | 1 / 9.88  ms | 4 / 10.27  ms | 4 / 15.41  ms |
| `s7b_insert_check_tenancy` | 3 / 20.32  ms | 3 / 17.07  ms | 4 / 32.85  ms | 4 / 25.67  ms |
