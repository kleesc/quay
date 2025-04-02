"""Create manifest jsonb columns

Revision ID: 76e3c52e801a
Revises: 3634f2df3c5b
Create Date: 2025-03-26 18:50:28.512903

"""

# revision identifiers, used by Alembic.
revision = '76e3c52e801a'
down_revision = '3634f2df3c5b'

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql
from sqlalchemy.engine.reflection import Inspector

def upgrade(op, tables, tester):
    bind = op.get_bind()
    if bind.engine.name == "postgresql":
        op.add_column(
            "manifest", sa.Column("manifest_json", postgresql.JSONB())
        )

    op.create_table(
        "modelcard",
        sa.Column("id", sa.Integer, nullable=False),
        sa.Column("manifest_id", sa.Integer, nullable=False),
        sa.PrimaryKeyConstraint("id", name=op.f("pk_modelcard")),
        sa.ForeignKeyConstraint(["manifest_id"], ["manifest.id"], name=op.f("fk_modelcard_manifest_id_manifest")),
    )


def downgrade(op, tables, tester):
    bind = op.get_bind()

    if bind.engine.name == "postgresql":
        op.drop_column("manifest", "manifest_json")

    op.drop_table("modelcard")
