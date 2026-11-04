"""Whole-checkpoint taxonomies keep custom class IDs separate from COCO IDs."""

BROAD_LABELS = ("bottle", "can", "cup", "plastic_bag", "paper", "cardboard",
                "drink_carton", "plastic_wrapper", "food_container", "bottle_cap")

BROAD_OBJECT_KEYS = {
    "bottle": ("pet_bottle", "hdpe_bottle", "glass_bottle"),
    "can": ("aluminium_can", "steel_can"),
    "cup": ("plastic_cup", "glass_cup", "ceramic_cup", "metal_cup", "paper_cup"),
    "plastic_bag": ("plastic_bag",),
    "paper": ("paper",),
    "cardboard": ("cardboard",),
    "drink_carton": ("drink_carton",),
    "plastic_wrapper": ("plastic_wrapper",),
    "food_container": ("plastic_food_container",),
    "bottle_cap": ("plastic_bottle_cap", "metal_bottle_cap"),
}
