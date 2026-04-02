def normal_row_count(model_cls, db):
    """Uses a normal .count() to retrieve the row count for a model."""
    return model_cls.select().count()
