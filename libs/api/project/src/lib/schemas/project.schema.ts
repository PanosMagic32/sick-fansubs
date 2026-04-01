import { Prop, Schema, SchemaFactory } from '@nestjs/mongoose';
import mongoose, { Document } from 'mongoose';

import { User } from '@api/user';

export type ProjectDocument = Project & Document;

@Schema({ collection: 'projects', timestamps: true })
export class Project {
  @Prop({ required: true })
  title!: string;

  @Prop({ required: true, unique: true })
  slug!: string;

  @Prop({ required: true })
  description!: string;

  @Prop({ required: true })
  thumbnail!: string;

  @Prop({ required: true })
  dateTimeCreated!: string;

  @Prop({ type: mongoose.Schema.Types.ObjectId, ref: 'User' })
  creator!: User;

  @Prop({ type: mongoose.Schema.Types.ObjectId, ref: 'User' })
  updatedBy?: User;

  @Prop()
  batchDownloadLinks!: string[];
}

export const ProjectSchema = SchemaFactory.createForClass(Project);
